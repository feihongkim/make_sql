package console

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// Env 는 전역 설정 인스턴스 (init() 이후 사용 가능)
var Env = &EnvType{}
var EnvHan = &DbHanEnv{}
var EnvVar = &DbVarEnv{}
var EnvKIS = &DbKISEnv{}
var EnvMong = &DbMongEnv{}

// EnvType 은 config.yaml의 설정값을 담는 구조체
type EnvType struct {
	// MSSQL 설정 (암호화됨)
	FKEY           string `yaml:"FKEY"`
	MSSQL_PORT     string `yaml:"MSSQL_PORT"`
	MSSQL_USER     string `yaml:"MSSQL_USER"`
	MSSQL_PASSWORD string `yaml:"MSSQL_PASSWORD"`


	// MSSQL 서버 주소 (서버이름 → 복호화된 주소)
	MSSQLAddrs map[string]string `yaml:"-"`

	// RabbitMQ 설정 (선택적 - 기본값 제공)
	RABBITMQ_HOST  string   `yaml:"RABBITMQ_HOST"`
	RABBITMQ_PORT  string   `yaml:"RABBITMQ_PORT"`
	RABBITMQ_USER  string   `yaml:"RABBITMQ_USER"`
	RABBITMQ_PASS  string   `yaml:"RABBITMQ_PASS"`
	RABBITMQ_QUEUE []string `yaml:"RABBITMQ_QUEUE"`

	// 프로젝트 설정
	LOGGER_PROJECT_NAME string `yaml:"LOGGER_PROJECT_NAME"`

	// 로그 레벨 설정 (DEBUG, INFO, ERROR, TEST)
	LOG_LEVEL string `yaml:"LOG_LEVEL"`

	// Telegram 알림 설정
	TELEGRAM_ENABLED bool `yaml:"TELEGRAM_ENABLED"`
}

// GetMSSQLAddr 는 서버 이름으로 MSSQL 주소를 반환합니다
func (e *EnvType) GetMSSQLAddr(serverName string) (string, error) {
	addr, ok := e.MSSQLAddrs[serverName]
	if !ok {
		names := make([]string, 0, len(e.MSSQLAddrs))
		for k := range e.MSSQLAddrs {
			names = append(names, k)
		}
		return "", fmt.Errorf("MSSQL 서버 '%s'을(를) 찾을 수 없습니다. 사용 가능: %v", serverName, names)
	}
	return addr, nil
}

// FirstMSSQLAddr 는 첫 번째 MSSQL 서버 주소를 반환합니다
func (e *EnvType) FirstMSSQLAddr() string {
	for _, addr := range e.MSSQLAddrs {
		return addr
	}
	return ""
}

type DbHanEnv struct {
	MSSQL_ADDR  string
	MSSQL_DBHan string
}

type DbVarEnv struct {
	MSSQL_ADDR  string
	MSSQL_DBVar string
}

type DbKISEnv struct {
	MSSQL_ADDR  string
	MSSQL_DBKIS string
}

type DbMongEnv struct {
	Mongo_ADDR string
	Mongo_PORT string
}

// LogMessage 는 RabbitMQ로 전송되는 로그 메시지 구조체
type LogMessage struct {
	Sender string `json:"Sender"`
	Time   string `json:"Time"`
	Msg    string `json:"Msg"`
}

// dbConfigs 는 DB 설정 조회용 내부 구조체
type dbConfigs struct {
	KEY_NAME   string
	VALUE_DATA string
}

// findProjectRoot walks up from the current directory looking for go.mod
// to locate the project root. Returns the path or an error if not found.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// loadConfig 는 config.yaml을 읽고 복호화 및 기본값 설정을 수행합니다
func loadConfig() error {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		// config.yaml not in CWD (e.g., running via go test) — find project root and chdir
		root, rootErr := findProjectRoot()
		if rootErr != nil {
			return fmt.Errorf("config.yaml 파일 읽기 실패: %w", err)
		}
		if chErr := os.Chdir(root); chErr != nil {
			return fmt.Errorf("프로젝트 루트 이동 실패: %w", chErr)
		}
		data, err = os.ReadFile("config.yaml")
		if err != nil {
			return fmt.Errorf("config.yaml 파일 읽기 실패 (루트: %s): %w", root, err)
		}
	}

	if err := yaml.Unmarshal(data, Env); err != nil {
		return fmt.Errorf("YAML 파싱 실패: %w", err)
	}

	// MSSQL_ADDR_* 키 파싱 (raw map으로)
	var rawMap map[string]interface{}
	if err := yaml.Unmarshal(data, &rawMap); err != nil {
		return fmt.Errorf("YAML raw 파싱 실패: %w", err)
	}

	// 기본값 설정
	setDefaults()

	// 암호화된 필드 복호화
	mainKey := GetKey(*Env)
	Env.MSSQL_USER = GetDecode(Env.MSSQL_USER, mainKey)
	Env.MSSQL_PASSWORD = GetDecode(Env.MSSQL_PASSWORD, mainKey)
	Env.MSSQL_PORT = GetDecode(Env.MSSQL_PORT, mainKey)
	// MSSQL_ADDR_서버이름 복호화
	Env.MSSQLAddrs = make(map[string]string)
	for key, val := range rawMap {
		if strings.HasPrefix(key, "MSSQL_ADDR_") {
			serverName := strings.TrimPrefix(key, "MSSQL_ADDR_")
			if encAddr, ok := val.(string); ok {
				Env.MSSQLAddrs[serverName] = GetDecode(encAddr, mainKey)
			}
		}
	}

	// 로그 레벨 초기화
	SetLogLevel(Env.LOG_LEVEL)

	return nil
}

// setDefaults 는 설정되지 않은 값에 기본값을 적용합니다
func setDefaults() {
	if Env.RABBITMQ_HOST == "" {
		Env.RABBITMQ_HOST = "localhost"
	}
	if Env.RABBITMQ_PORT == "" {
		Env.RABBITMQ_PORT = "5672"
	}
	if Env.RABBITMQ_USER == "" {
		Env.RABBITMQ_USER = "guest"
	}
	if Env.RABBITMQ_PASS == "" {
		Env.RABBITMQ_PASS = "guest"
	}
	if len(Env.RABBITMQ_QUEUE) == 0 {
		Env.RABBITMQ_QUEUE = []string{"slice2DB", "FEILOGIC", "LOG", "KISData"}
	}
	if Env.LOGGER_PROJECT_NAME == "" {
		Env.LOGGER_PROJECT_NAME = "KIS"
	}
	if Env.LOG_LEVEL == "" {
		Env.LOG_LEVEL = "DEBUG"
	}
	// TELEGRAM_ENABLED 기본값: true
	// (YAML에 없으면 기본적으로 Telegram 활성화)
}

// GenerateTimestampedString 은 로그용 타임스탬프 문자열을 생성합니다
func GenerateTimestampedString() string {
	systemTZ, err := time.LoadLocation("Local")
	if err != nil {
		systemTZ = time.UTC
	}

	currentTime := time.Now().In(systemTZ)

	// UTC이면 9시간 추가 (서울 기준)
	_, offset := currentTime.Zone()
	if offset == 0 {
		currentTime = currentTime.Add(9 * time.Hour)
	}

	return currentTime.Format("20060102_150405")
}

// getDBConfigs 는 DB에서 설정값을 조회합니다 (내부 사용)
func getDBConfigs(msConn *sql.DB, Gubun string) ([]dbConfigs, error) {
	query := `SELECT kvs.KEY_NAME, kvs.VALUE_DATA FROM KeyValueStore kvs WHERE kvs.GUBUN = @p1`

	rows, err := msConn.Query(query, Gubun)
	if err != nil {
		return nil, fmt.Errorf("쿼리 실행 오류: %w", err)
	}
	defer rows.Close()

	var result []dbConfigs
	for rows.Next() {
		var item dbConfigs
		if err := rows.Scan(&item.KEY_NAME, &item.VALUE_DATA); err != nil {
			return nil, fmt.Errorf("row 스캔 오류: %w", err)
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row 반복 오류: %w", err)
	}
	return result, nil
}
