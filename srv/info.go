package srv

import (
	"context"
	"fmt"
	"log"

	"MakeSQL/console"
)

// HandleInfo 는 기본 동작: 모든 MongoDB 연결 정보를 출력합니다
func HandleInfo(ctx context.Context, cfg *console.Config) {
	mongoCfg := cfg.MongoDB
	for _, conn := range mongoCfg.Connections {
		uri := fmt.Sprintf("mongodb://%s:%d/?retryWrites=%t&w=%s",
			conn.URI,
			mongoCfg.Options.Port,
			mongoCfg.Options.RetryWrites,
			mongoCfg.Options.W,
		)
		client, err := console.ConnectMongo(
			ctx,
			uri,
			mongoCfg.Options.ConnectTimeoutMS,
			mongoCfg.Options.SocketTimeoutMS,
		)
		if err != nil {
			log.Printf("[%s] 연결 실패: %v\n", conn.Name, err)
			continue
		}

		console.PrintDBInfo(ctx, client, conn.Name)

		if err := client.Disconnect(ctx); err != nil {
			log.Printf("[%s] 연결 해제 실패: %v\n", conn.Name, err)
		}
	}

	fmt.Println("\n완료.")
}
