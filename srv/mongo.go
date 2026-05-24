package srv

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"MakeSQL/console"
)

// HandleMongo 는 mongo 서브커맨드를 처리합니다
// ./abledb mongo [연결이름] --dblist
// ./abledb mongo [연결이름] --drop-before [날짜]
// ./abledb mongo [연결이름] [DB이름] [JSON 명령어 또는 @파일명]
func HandleMongo(ctx context.Context, cfg *console.Config, args []string) {
	if len(args) < 2 {
		fmt.Println("사용법:")
		fmt.Println("  ./abledb mongo [연결이름] --dblist")
		fmt.Println("  ./abledb mongo [연결이름] --drop-before [날짜]")
		fmt.Println("  ./abledb mongo [연결이름] [DB이름] [JSON 명령어]")
		fmt.Println("  ./abledb mongo [연결이름] [DB이름] @파일명    (mongo/ 폴더의 .json 파일)")
		os.Exit(1)
	}

	connName := args[0]

	uri, err := console.BuildMongoURI(cfg, connName)
	if err != nil {
		log.Fatalf("%v", err)
	}

	client, err := console.ConnectMongo(
		ctx, uri,
		cfg.MongoDB.Options.ConnectTimeoutMS,
		cfg.MongoDB.Options.SocketTimeoutMS,
	)
	if err != nil {
		log.Fatalf("[%s] 연결 실패: %v", connName, err)
	}
	defer client.Disconnect(ctx)

	switch args[1] {
	case "--dblist":
		console.PrintDBInfo(ctx, client, connName)
		return

	case "--drop-before":
		if len(args) < 3 {
			fmt.Println("사용법: ./abledb mongo [연결이름] --drop-before [YYYY-MM-DD]")
			os.Exit(1)
		}
		dropped, err := console.DropCollectionsBefore(ctx, client, connName, args[2])
		if err != nil {
			log.Fatalf("[%s] %v", connName, err)
		}
		fmt.Printf("\n총 %d개 컬렉션 삭제 완료.\n", dropped)
		return
	}

	// 명령어 실행
	if len(args) < 3 {
		fmt.Println("사용법: ./abledb mongo [연결이름] [DB이름] [JSON 명령어 또는 @파일명]")
		os.Exit(1)
	}
	dbName := args[1]
	commandJSON := resolveMongoQuery(args[2])

	result, err := console.RunMongoCommand(ctx, client, dbName, commandJSON)
	if err != nil {
		log.Fatalf("[%s/%s] %v", connName, dbName, err)
	}
	fmt.Println(result)
}

// resolveMongoQuery 는 @파일명이면 mongo/ 폴더에서 파일을 읽고, 아니면 그대로 반환합니다
func resolveMongoQuery(input string) string {
	if !strings.HasPrefix(input, "@") {
		return input
	}

	filename := strings.TrimPrefix(input, "@")
	if !strings.HasSuffix(filename, ".json") {
		filename += ".json"
	}

	data, err := os.ReadFile("mongo/" + filename)
	if err != nil {
		log.Fatalf("MongoDB 쿼리 파일 읽기 실패: mongo/%s: %v", filename, err)
	}
	return string(data)
}
