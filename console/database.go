package console

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
)

type msConn struct {
	db         map[string]*sql.DB
	lock       sync.RWMutex
	healthOnce sync.Once
}

var (
	MsConn *msConn
	dbOnce sync.Once
)

// initMsConn 싱글턴 객체 반환 (init.go에서 호출)
func initMsConn() *msConn {
	dbOnce.Do(func() {
		MsConn = &msConn{
			db: make(map[string]*sql.DB),
		}
	})
	return MsConn
}

// BuildConnStr 은 서버 주소와 DB 이름으로 연결 문자열을 생성합니다
func BuildConnStr(serverAddr, database string) string {
	return fmt.Sprintf("server=%s;user id=%s;password=%s;database=%s;encrypt=disable;trustServerCertificate=true;connection timeout=3",
		serverAddr, Env.MSSQL_USER, Env.MSSQL_PASSWORD, database)
}

// ConnectMSSQL 은 서버이름과 DB이름으로 직접 연결합니다
// dbKey 형식: "서버이름:DB이름" (예: "ITWdesk:mydb")
func (m *msConn) ConnectMSSQL(serverName, database string) error {
	addr, err := Env.GetMSSQLAddr(serverName)
	if err != nil {
		return err
	}
	dbKey := serverName + ":" + database
	connStr := BuildConnStr(addr, database)
	return m.connectWithStr(dbKey, connStr)
}

// DB 연결 생성 및 등록
func (m *msConn) initDB(dbname string) error {
	var connStr string

	switch dbname {
	case "key":
		// 첫 번째 등록된 MSSQL 서버의 DBKEY에 연결
		connStr = BuildConnStr(Env.FirstMSSQLAddr(), Env.MSSQL_DBKEY)
	case "han":
		connStr = BuildConnStr(EnvHan.MSSQL_ADDR, EnvHan.MSSQL_DBHan)
	case "var":
		connStr = BuildConnStr(EnvVar.MSSQL_ADDR, EnvVar.MSSQL_DBVar)
	case "KIS2":
		connStr = BuildConnStr(EnvKIS.MSSQL_ADDR, EnvKIS.MSSQL_DBKIS)
	default:
		// "서버이름:DB이름" 형식 지원
		parts := splitDBKey(dbname)
		if parts != nil {
			addr, err := Env.GetMSSQLAddr(parts[0])
			if err != nil {
				return err
			}
			connStr = BuildConnStr(addr, parts[1])
		} else {
			return fmt.Errorf("알 수 없는 DB 이름: %s (형식: 서버이름:DB이름)", dbname)
		}
	}

	return m.connectWithStr(dbname, connStr)
}

// splitDBKey 는 "서버:DB" 형식을 분리합니다
func splitDBKey(key string) []string {
	idx := -1
	for i, c := range key {
		if c == ':' {
			idx = i
			break
		}
	}
	if idx > 0 && idx < len(key)-1 {
		return []string{key[:idx], key[idx+1:]}
	}
	return nil
}

func (m *msConn) connectWithStr(dbname, connStr string) error {

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return fmt.Errorf("DB 열기 실패: %w", err)
	}

	db.SetConnMaxIdleTime(10 * time.Minute)
	db.SetConnMaxLifetime(1 * time.Hour)
	db.SetMaxIdleConns(20) // 비동기 MERGE worker pool 지원: 10 → 20
	db.SetMaxOpenConns(100) // 성능 최적화: 50 → 100 (배치 MERGE 지원)

	for i := 0; i < 3; i++ {
		if err := db.Ping(); err == nil {
			m.db[dbname] = db
			Log("[MsConn] [%s] DB 연결 완료", dbname)
			return nil
		}
		LogError("[MsConn] [%s] Ping 실패 (%d/3), 재시도 중...", dbname, i+1)
		time.Sleep(2 * time.Second)
	}

	_ = db.Close()
	return fmt.Errorf("DB Ping 3회 실패: %s", dbname)
}

func (m *msConn) EnsureConnection(dbname string) error {
	m.lock.RLock()
	db, ok := m.db[dbname]
	defer m.lock.RUnlock()

	if ok {
		if err := db.Ping(); err == nil {
			return nil
		}
		LogError("[MsConn] [%s] Ping 실패, 재연결 시도 중...", dbname)
		_ = db.Close()
	}

	// 새 연결 시도
	if err := m.initDB(dbname); err != nil {
		delete(m.db, dbname)
		return err
	}
	return nil
}

// GetDB DB 인스턴스 안전하게 반환
func (m *msConn) GetDB(dbname string) (*sql.DB, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	db, ok := m.db[dbname]
	if !ok {
		return nil, fmt.Errorf("DB '%s'가 등록되지 않았습니다", dbname)
	}
	return db, nil
}

// ListMSSQLDatabases 는 서버의 DB 목록을 반환합니다
func ListMSSQLDatabases(serverName string) ([]string, error) {
	addr, err := Env.GetMSSQLAddr(serverName)
	if err != nil {
		return nil, err
	}

	connStr := BuildConnStr(addr, "master")
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("연결 실패: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT name FROM sys.databases ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("쿼리 실패: %w", err)
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}

// ListMSSQLTables 는 특정 DB의 스키마와 테이블 목록을 반환합니다
func ListMSSQLTables(serverName, dbName string) ([]string, error) {
	addr, err := Env.GetMSSQLAddr(serverName)
	if err != nil {
		return nil, err
	}

	connStr := BuildConnStr(addr, dbName)
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("연결 실패: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT TABLE_SCHEMA, TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_SCHEMA, TABLE_NAME`)
	if err != nil {
		return nil, fmt.Errorf("쿼리 실패: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return nil, err
		}
		tables = append(tables, schema+"."+table)
	}
	return tables, rows.Err()
}

// PrintMSSQLInfo 는 서버의 전체 DB/스키마/테이블 정보를 출력합니다
func PrintMSSQLInfo(serverName string) error {
	dbs, err := ListMSSQLDatabases(serverName)
	if err != nil {
		return err
	}

	fmt.Printf("\n=== [%s] MSSQL ===\n", serverName)
	for _, dbName := range dbs {
		// 시스템 DB 건너뛰기
		if dbName == "master" || dbName == "tempdb" || dbName == "model" || dbName == "msdb" {
			continue
		}
		fmt.Printf("\n  DB: %s\n", dbName)
		tables, err := ListMSSQLTables(serverName, dbName)
		if err != nil {
			fmt.Printf("    (테이블 조회 실패: %v)\n", err)
			continue
		}
		if len(tables) == 0 {
			fmt.Println("    (테이블 없음)")
			continue
		}
		for _, t := range tables {
			fmt.Printf("    - %s\n", t)
		}
	}
	return nil
}

// RunMSSQLQuery 는 서버의 특정 DB에서 쿼리를 실행하고 결과를 반환합니다
func RunMSSQLQuery(serverName, dbName, query string) (string, error) {
	addr, err := Env.GetMSSQLAddr(serverName)
	if err != nil {
		return "", err
	}

	connStr := BuildConnStr(addr, dbName)
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return "", fmt.Errorf("연결 실패: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return "", fmt.Errorf("쿼리 실패: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var result string
	// 헤더
	for i, col := range cols {
		if i > 0 {
			result += "\t"
		}
		result += col
	}
	result += "\n"

	// 데이터
	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return "", err
		}
		for i, v := range values {
			if i > 0 {
				result += "\t"
			}
			if v == nil {
				result += "NULL"
			} else {
				result += fmt.Sprintf("%v", v)
			}
		}
		result += "\n"
	}
	return result, rows.Err()
}

// QueryMSSQLRows 는 서버의 특정 DB에서 쿼리를 실행하고 결과를 []map[string]string 으로 반환합니다
func QueryMSSQLRows(serverName, dbName, query string) ([]map[string]string, error) {
	addr, err := Env.GetMSSQLAddr(serverName)
	if err != nil {
		return nil, err
	}

	connStr := BuildConnStr(addr, dbName)
	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("연결 실패: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("쿼리 실패: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var result []map[string]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		row := make(map[string]string, len(cols))
		for i, col := range cols {
			v := values[i]
			if v == nil {
				row[col] = ""
			} else if b, ok := v.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = fmt.Sprintf("%v", v)
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// Close 모든 DB 연결 종료
func (m *msConn) Close() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if len(m.db) == 0 {
		Log("[MsConn] 연결된 DB가 없습니다")
		return nil
	}

	var lastErr error
	for dbname, db := range m.db {
		if err := db.Close(); err != nil {
			LogError("[MsConn] [%s] DB 연결 종료 실패: %v", dbname, err)
			lastErr = err
		} else {
			Log("[MsConn] [%s] DB 연결 종료", dbname)
		}
		delete(m.db, dbname)
	}
	return lastErr
}
