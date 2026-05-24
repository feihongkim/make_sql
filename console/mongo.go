package console

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Config struct {
	MongoDB struct {
		Options struct {
			RetryWrites      bool   `yaml:"retryWrites"`
			W                string `yaml:"w"`
			ConnectTimeoutMS int    `yaml:"connectTimeoutMS"`
			SocketTimeoutMS  int    `yaml:"socketTimeoutMS"`
			Port             int    `yaml:"port"`
		} `yaml:"options"`
		Connections []struct {
			Name string `yaml:"name"`
			URI  string `yaml:"uri"`
		} `yaml:"connections"`
	} `yaml:"mongodb"`
}

func ConnectMongo(ctx context.Context, uri string, connectTimeout, socketTimeout int) (*mongo.Client, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetConnectTimeout(time.Duration(connectTimeout) * time.Millisecond).
		SetSocketTimeout(time.Duration(socketTimeout) * time.Millisecond)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("MongoDB 연결 실패: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("MongoDB ping 실패: %w", err)
	}

	return client, nil
}

// BuildMongoURI 는 Config에서 연결 이름으로 MongoDB URI를 생성합니다
func BuildMongoURI(cfg *Config, connName string) (string, error) {
	mongoCfg := cfg.MongoDB
	for _, conn := range mongoCfg.Connections {
		if conn.Name == connName {
			return fmt.Sprintf("mongodb://%s:%d/?retryWrites=%t&w=%s",
				conn.URI,
				mongoCfg.Options.Port,
				mongoCfg.Options.RetryWrites,
				mongoCfg.Options.W,
			), nil
		}
	}
	names := make([]string, len(mongoCfg.Connections))
	for i, c := range mongoCfg.Connections {
		names[i] = c.Name
	}
	return "", fmt.Errorf("연결 이름 '%s'을(를) 찾을 수 없습니다. 사용 가능: %v", connName, names)
}

// RunMongoCommand 는 지정된 DB에서 JSON 형태의 MongoDB 명령어를 실행합니다
func RunMongoCommand(ctx context.Context, client *mongo.Client, dbName string, commandJSON string) (string, error) {
	var cmd bson.D
	if err := bson.UnmarshalExtJSON([]byte(commandJSON), true, &cmd); err != nil {
		return "", fmt.Errorf("JSON 파싱 실패: %w", err)
	}

	var result bson.M
	if err := client.Database(dbName).RunCommand(ctx, cmd).Decode(&result); err != nil {
		return "", fmt.Errorf("명령 실행 실패: %w", err)
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("결과 직렬화 실패: %w", err)
	}
	return string(output), nil
}

// normalizeDate 는 다양한 날짜 형식을 YYYY-MM-DD로 통일합니다
// 지원: "YYYY-MM-DD", "YYYY/MM/DD", "YYYYMMDD", "YYYY-MM-DD HH...", "YYYY/MM/DD HH..."
func normalizeDate(name string) (string, bool) {
	// 앞 10자리만 추출 (뒤에 시간 등이 붙을 수 있음)
	// YYYY-MM-DD ... 또는 YYYY/MM/DD ...
	if matched, _ := regexp.MatchString(`^\d{4}[-/]\d{2}[-/]\d{2}`, name); matched {
		dateStr := strings.ReplaceAll(name[:10], "/", "-")
		return dateStr, true
	}
	// YYYYMMDD → YYYY-MM-DD
	if matched, _ := regexp.MatchString(`^\d{8}$`, name); matched {
		return name[:4] + "-" + name[4:6] + "-" + name[6:8], true
	}
	return "", false
}

// DropCollectionsBefore 는 모든 DB에서 날짜 형식의 컬렉션 중 cutoffDate 이전 것을 삭제합니다
// cutoffDate 및 컬렉션 이름 모두 다양한 형식 지원: YYYY-MM-DD, YYYY/MM/DD, YYYYMMDD (+ 시간 접미사)
func DropCollectionsBefore(ctx context.Context, client *mongo.Client, connName string, cutoffDate string) (int, error) {
	// 입력된 cutoffDate도 정규화
	if normalized, ok := normalizeDate(cutoffDate); ok {
		cutoffDate = normalized
	} else {
		return 0, fmt.Errorf("잘못된 날짜 형식: %s (YYYY-MM-DD, YYYY/MM/DD, YYYYMMDD 지원)", cutoffDate)
	}
	databases, err := client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		return 0, fmt.Errorf("DB 목록 조회 실패: %w", err)
	}

	dropped := 0
	for _, dbName := range databases {
		if dbName == "admin" || dbName == "config" || dbName == "local" {
			continue
		}
		db := client.Database(dbName)
		collections, err := db.ListCollectionNames(ctx, bson.M{})
		if err != nil {
			log.Printf("  [%s/%s] 컬렉션 목록 조회 실패: %v", connName, dbName, err)
			continue
		}
		for _, colName := range collections {
			normalized, ok := normalizeDate(colName)
			if !ok {
				continue
			}
			if normalized < cutoffDate {
				if err := db.Collection(colName).Drop(ctx); err != nil {
					log.Printf("  [%s/%s/%s] 삭제 실패: %v", connName, dbName, colName, err)
				} else {
					fmt.Printf("  삭제: %s / %s\n", dbName, colName)
					dropped++
				}
			}
		}
	}
	return dropped, nil
}

func PrintDBInfo(ctx context.Context, client *mongo.Client, name string) {
	fmt.Printf("\n=== [%s] ===\n", name)

	databases, err := client.ListDatabaseNames(ctx, bson.M{})
	if err != nil {
		log.Printf("[%s] DB 목록 조회 실패: %v\n", name, err)
		return
	}

	for _, dbName := range databases {
		fmt.Printf("\n  DB: %s\n", dbName)
		db := client.Database(dbName)

		collections, err := db.ListCollectionNames(ctx, bson.M{})
		if err != nil {
			log.Printf("  [%s/%s] 컬렉션 목록 조회 실패: %v\n", name, dbName, err)
			continue
		}

		if len(collections) == 0 {
			fmt.Println("    (컬렉션 없음)")
			continue
		}

		for _, colName := range collections {
			count, err := db.Collection(colName).EstimatedDocumentCount(ctx)
			if err != nil {
				fmt.Printf("    - %s (문서 수 조회 실패)\n", colName)
				continue
			}
			fmt.Printf("    - %s (%d documents)\n", colName, count)
		}
	}
}
