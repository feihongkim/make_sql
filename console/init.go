package console

import (
	"fmt"
	"os"
)

// init 은 패키지의 유일한 init() 함수입니다.
// main() 실행 전에 자동으로 호출되어 모든 초기화를 수행합니다.
func init() {
	// Step 1: Config 로드 (config.yaml 읽기 + 복호화)
	if err := loadConfig(); err != nil {
		fmt.Printf("%s [Init] Config 로딩 실패: %v\n", GenerateTimestampedString(), err)
		os.Exit(1)
	}
	fmt.Printf("%s [Init] Config 로드 완료\n", GenerateTimestampedString())

	// Step 2: RabbitMQ 세션 초기화
	if err := initRabbitMQSession(); err != nil {
		fmt.Printf("%s [Init] RabbitMQ 세션 초기화 실패: %v\n", GenerateTimestampedString(), err)
		os.Exit(1)
	}
	for _, queueName := range Env.RABBITMQ_QUEUE {
		if err := RabbitMQSession.AddChannelAndQueue(queueName); err != nil {
			fmt.Printf("%s [Init] RabbitMQ 채널/큐 설정 실패 (%s): %v\n", GenerateTimestampedString(), queueName, err)
			os.Exit(1)
		}
	}
	fmt.Printf("%s [Init] RabbitMQ 세션 초기화 완료\n", GenerateTimestampedString())

	// Step 3: 로거 배치 프로세서 시작
	initLogger()
	Log("[Init] 로거 배치 프로세서 시작")

	// Step 4: RabbitMQ 모니터링 고루틴 시작
	go RabbitMQSession.MonitorConnection()
	go RabbitMQSession.HandleReconnection()

	// Step 5: MSSQL 연결 초기화
	initMsConn()

	// key DB 연결 (실패 시 경고만, 프로세스 종료 안함)
	keyOK := true
	if err := MsConn.EnsureConnection("key"); err != nil {
		LogError("[Init] key DB 연결 실패 (스킵): %v", err)
		keyOK = false
	}

	// key DB에서 추가 설정 로드 (key 연결 성공 시에만)
	if keyOK {
		db, err := MsConn.GetDB("key")
		if err == nil {
			configs, err := getDBConfigs(db, "DB")
			if err != nil {
				LogError("[Init] DB 설정 로드 실패 (스킵): %v", err)
			} else {
				for _, config := range configs {
					switch config.KEY_NAME {
					case "ADDR_VAR":
						EnvVar.MSSQL_ADDR = config.VALUE_DATA
					case "DBNAME_VAR":
						EnvVar.MSSQL_DBVar = config.VALUE_DATA
					case "ADDR_HAN":
						EnvHan.MSSQL_ADDR = config.VALUE_DATA
					case "DBNAME_HAN":
						EnvHan.MSSQL_DBHan = config.VALUE_DATA
					case "ADDR_KIS":
						EnvKIS.MSSQL_ADDR = config.VALUE_DATA
					case "DBNAME_KIS":
						EnvKIS.MSSQL_DBKIS = config.VALUE_DATA
					case "ADDR_MONG":
						EnvMong.Mongo_ADDR = config.VALUE_DATA
					case "PORT_MONG":
						EnvMong.Mongo_PORT = config.VALUE_DATA
					}
				}
			}
		}
	}

	// KIS DB 설정 미등록 시 han과 동일 서버 사용
	if EnvKIS.MSSQL_ADDR == "" {
		EnvKIS.MSSQL_ADDR = EnvHan.MSSQL_ADDR
	}
	if EnvKIS.MSSQL_DBKIS == "" {
		EnvKIS.MSSQL_DBKIS = "KIS2"
	}

	// 나머지 DB 연결 (실패 시 경고만, 프로세스 종료 안함)
	for _, dbname := range []string{"han", "var", "KIS2"} {
		if err := MsConn.EnsureConnection(dbname); err != nil {
			LogError("[Init] %s DB 연결 실패 (스킵): %v", dbname, err)
		}
	}
	Log("[Init] MSSQL 연결 초기화 완료")

	Log("[Init] 모든 초기화 완료 - main() 준비됨")
}
