package srv

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"MakeSQL/console"
)

// HandleCopy 는 서버 간 테이블 데이터 복사를 처리합니다
// ./abledb copy [소스서버] [소스DB] [대상서버] [대상DB] [테이블] [WHERE조건]
// ./abledb copy --upsert [소스서버] [소스DB] [대상서버] [대상DB] [테이블] [WHERE조건] [PK컬럼들(콤마구분)]
func HandleCopy(ctx context.Context, args []string) {
	upsertMode := false
	if len(args) > 0 && args[0] == "--upsert" {
		upsertMode = true
		args = args[1:]
	}

	minArgs := 6
	if upsertMode {
		minArgs = 7
	}

	if len(args) < minArgs {
		fmt.Println("사용법:")
		fmt.Println("  ./abledb copy [소스서버] [소스DB] [대상서버] [대상DB] [테이블] [WHERE조건]")
		fmt.Println("  ./abledb copy --upsert [소스서버] [소스DB] [대상서버] [대상DB] [테이블] [WHERE조건] [PK컬럼(콤마구분)]")
		fmt.Println("예:")
		fmt.Println("  ./abledb copy ITWdesk hannam white hannam stock_price_kor_d001 \"DATE > '20260306'\"")
		fmt.Println("  ./abledb copy --upsert ITWdesk hannam white hannam stock_code \"1=1\" \"COUNTRY,MARKET,SHCODE\"")
		os.Exit(1)
	}

	srcServer := args[0]
	srcDB := args[1]
	dstServer := args[2]
	dstDB := args[3]
	tableName := args[4]
	whereClause := args[5]

	var pkCols []string
	if upsertMode {
		pkCols = strings.Split(args[6], ",")
	}

	// 소스 연결
	srcAddr, err := console.Env.GetMSSQLAddr(srcServer)
	if err != nil {
		log.Fatalf("소스 서버 주소 조회 실패: %v", err)
	}
	srcConn, err := sql.Open("sqlserver", console.BuildConnStr(srcAddr, srcDB))
	if err != nil {
		log.Fatalf("소스 연결 실패: %v", err)
	}
	defer srcConn.Close()

	// 대상 연결
	dstAddr, err := console.Env.GetMSSQLAddr(dstServer)
	if err != nil {
		log.Fatalf("대상 서버 주소 조회 실패: %v", err)
	}
	dstConn, err := sql.Open("sqlserver", console.BuildConnStr(dstAddr, dstDB))
	if err != nil {
		log.Fatalf("대상 연결 실패: %v", err)
	}
	defer dstConn.Close()

	// 소스에서 데이터 조회
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s", tableName, whereClause)
	rows, err := srcConn.QueryContext(ctx, query)
	if err != nil {
		log.Fatalf("소스 쿼리 실패: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Fatalf("컬럼 조회 실패: %v", err)
	}

	mode := "INSERT"
	if upsertMode {
		mode = "UPSERT"
	}
	fmt.Printf("[Copy:%s] %s.%s.%s → %s.%s.%s\n", mode, srcServer, srcDB, tableName, dstServer, dstDB, tableName)
	fmt.Printf("[Copy] WHERE %s\n", whereClause)
	if upsertMode {
		fmt.Printf("[Copy] PK: %s\n", strings.Join(pkCols, ", "))
	}
	fmt.Printf("[Copy] 컬럼: %s\n", strings.Join(cols, ", "))

	batchSize := 500
	var batch [][]any
	totalRows := 0
	insertedRows := 0
	updatedRows := 0
	skippedRows := 0

	for rows.Next() {
		values := make([]any, len(cols))
		valuePtrs := make([]any, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			log.Fatalf("스캔 실패: %v", err)
		}
		batch = append(batch, values)
		totalRows++

		if len(batch) >= batchSize {
			var ni, nu, ns int
			var berr error
			if upsertMode {
				ni, nu, ns, berr = upsertBatch(ctx, dstConn, tableName, cols, pkCols, batch)
			} else {
				ni, ns, berr = insertBatch(ctx, dstConn, tableName, cols, batch)
			}
			if berr != nil {
				log.Fatalf("배치 실패 (row %d): %v", totalRows, berr)
			}
			insertedRows += ni
			updatedRows += nu
			skippedRows += ns
			if upsertMode {
				fmt.Printf("\r[Copy] 진행: %d건 읽기, %d건 추가, %d건 업데이트", totalRows, insertedRows, updatedRows)
			} else {
				fmt.Printf("\r[Copy] 진행: %d건 읽기, %d건 삽입, %d건 스킵(중복)", totalRows, insertedRows, skippedRows)
			}
			batch = batch[:0]
		}
	}

	// 남은 배치
	if len(batch) > 0 {
		if upsertMode {
			ni, nu, _, berr := upsertBatch(ctx, dstConn, tableName, cols, pkCols, batch)
			if berr != nil {
				log.Fatalf("마지막 배치 실패: %v", berr)
			}
			insertedRows += ni
			updatedRows += nu
		} else {
			ni, ns, berr := insertBatch(ctx, dstConn, tableName, cols, batch)
			if berr != nil {
				log.Fatalf("마지막 배치 실패: %v", berr)
			}
			insertedRows += ni
			skippedRows += ns
		}
	}

	if upsertMode {
		fmt.Printf("\n[Copy] 완료: 총 %d건 읽기, %d건 추가, %d건 업데이트\n", totalRows, insertedRows, updatedRows)
	} else {
		fmt.Printf("\n[Copy] 완료: 총 %d건 읽기, %d건 삽입, %d건 스킵(중복)\n", totalRows, insertedRows, skippedRows)
	}
}

func insertBatch(ctx context.Context, db *sql.DB, table string, cols []string, batch [][]any) (int, int, error) {
	if len(batch) == 0 {
		return 0, 0, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}

	inserted := 0
	skipped := 0
	for _, row := range batch {
		placeholders := make([]string, len(cols))
		for i := range cols {
			placeholders[i] = fmt.Sprintf("@p%d", i+1)
		}
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			quotedCols[i] = "[" + c + "]"
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			table,
			strings.Join(quotedCols, ", "),
			strings.Join(placeholders, ", "))

		_, err := tx.ExecContext(ctx, query, row...)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") ||
				strings.Contains(err.Error(), "Violation of PRIMARY KEY") ||
				strings.Contains(err.Error(), "UNIQUE KEY") {
				skipped++
				continue
			}
			tx.Rollback()
			return inserted, skipped, fmt.Errorf("INSERT 실패: %w", err)
		}
		inserted++
	}

	if err := tx.Commit(); err != nil {
		return inserted, skipped, fmt.Errorf("커밋 실패: %w", err)
	}
	return inserted, skipped, nil
}

func upsertBatch(ctx context.Context, db *sql.DB, table string, cols []string, pkCols []string, batch [][]any) (int, int, int, error) {
	if len(batch) == 0 {
		return 0, 0, 0, nil
	}

	// PK 컬럼 인덱스 맵
	pkSet := make(map[string]bool)
	for _, pk := range pkCols {
		pkSet[strings.TrimSpace(pk)] = true
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("트랜잭션 시작 실패: %w", err)
	}

	inserted := 0
	updated := 0
	skipped := 0

	for _, row := range batch {
		placeholders := make([]string, len(cols))
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			placeholders[i] = fmt.Sprintf("@p%d", i+1)
			quotedCols[i] = "[" + c + "]"
		}

		// MERGE 문 생성
		var onClauses []string
		var updateClauses []string
		for i, c := range cols {
			if pkSet[c] {
				onClauses = append(onClauses, fmt.Sprintf("target.[%s] = source.[%s]", c, c))
			} else {
				updateClauses = append(updateClauses, fmt.Sprintf("target.[%s] = source.[%s]", c, c))
			}
			_ = i
		}

		// source 값 매핑
		var srcCols []string
		for i, c := range cols {
			srcCols = append(srcCols, fmt.Sprintf("%s AS [%s]", placeholders[i], c))
		}

		query := fmt.Sprintf(`
			MERGE %s AS target
			USING (SELECT %s) AS source
			ON %s
			WHEN MATCHED THEN UPDATE SET %s
			WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);`,
			table,
			strings.Join(srcCols, ", "),
			strings.Join(onClauses, " AND "),
			strings.Join(updateClauses, ", "),
			strings.Join(quotedCols, ", "),
			strings.Join(placeholders, ", "))

		result, err := tx.ExecContext(ctx, query, row...)
		if err != nil {
			tx.Rollback()
			return inserted, updated, skipped, fmt.Errorf("MERGE 실패: %w", err)
		}
		affected, _ := result.RowsAffected()
		if affected > 0 {
			// MERGE는 INSERT/UPDATE 구분이 어려우므로 일단 카운트
			inserted++ // 실제로는 insert+update 혼합
		}
	}

	if err := tx.Commit(); err != nil {
		return inserted, updated, skipped, fmt.Errorf("커밋 실패: %w", err)
	}
	// MERGE에서는 정확한 insert/update 구분이 어려우므로 총 처리 수를 inserted에 담음
	return inserted, updated, skipped, nil
}
