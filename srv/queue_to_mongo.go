package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"MakeSQL/console"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var queueNames = []string{"KIS2_STI_1", "KIS2_sector", "KIS2_theme"}

// RunQueueToMongo 는 RabbitMQ 큐에서 메시지를 consume하여 MongoDB(27017)에 저장한다.
// stopHour(KST)에 자동 종료된다. manual-ack으로 저장 성공 후에만 큐에서 제거.
func RunQueueToMongo(stopHour int) {
	loc, _ := time.LoadLocation("Asia/Seoul")

	// MongoDB 27017 연결
	ctx := context.Background()
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		console.LogError("[queue-to-mongo] MongoDB 연결 실패: %v", err)
		return
	}
	defer mongoClient.Disconnect(ctx)

	if err := mongoClient.Ping(ctx, nil); err != nil {
		console.LogError("[queue-to-mongo] MongoDB ping 실패: %v", err)
		return
	}
	console.Log("[queue-to-mongo] MongoDB(27017) 연결 성공")

	// RabbitMQ 큐 채널 등록
	for _, q := range queueNames {
		if err := console.RabbitMQSession.AddChannelAndQueue(q); err != nil {
			console.LogError("[queue-to-mongo] 큐 %s 등록 실패: %v", q, err)
			return
		}
	}
	console.Log("[queue-to-mongo] RabbitMQ 큐 3개 등록 완료")

	// 각 큐별 consume 시작
	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	for _, qName := range queueNames {
		wg.Add(1)
		go func(queue string) {
			defer wg.Done()
			consumeQueue(ctx, mongoClient, queue, stopCh, loc)
		}(qName)
	}

	console.Log("[queue-to-mongo] consume 시작 (KST %02d:00에 자동 종료)", stopHour)

	// stopHour까지 대기
	for {
		now := time.Now().In(loc)
		if now.Hour() >= stopHour {
			console.Log("[queue-to-mongo] KST %02d시 도달 — 종료", stopHour)
			close(stopCh)
			break
		}
		time.Sleep(30 * time.Second)
	}

	wg.Wait()

	// 큐 채널 정리
	for _, q := range queueNames {
		console.RabbitMQSession.RemoveChannelAndQueue(q)
	}
	console.Log("[queue-to-mongo] 완료")
}

func consumeQueue(ctx context.Context, client *mongo.Client, queueName string, stopCh <-chan struct{}, loc *time.Location) {
	msgChan := make(chan amqp.Delivery, 100)
	if err := console.RabbitMQSession.ReceiveManualAck(queueName, msgChan); err != nil {
		console.LogError("[queue-to-mongo] %s receive 실패: %v", queueName, err)
		return
	}

	db := client.Database(queueName)
	count := 0

	for {
		select {
		case <-stopCh:
			console.Log("[queue-to-mongo] %s: %d건 저장 완료", queueName, count)
			return
		case delivery := <-msgChan:
			now := time.Now().In(loc)
			collName := now.Format("060102") // YYMMDD

			var doc any
			if err := json.Unmarshal(delivery.Body, &doc); err != nil {
				doc = map[string]any{
					"raw":       string(delivery.Body),
					"timestamp": now,
				}
			}

			coll := db.Collection(collName)
			if _, err := coll.InsertOne(ctx, doc); err != nil {
				console.LogError("[queue-to-mongo] %s insert 실패: %v", queueName, err)
				delivery.Nack(false, true) // 저장 실패 → 큐에 다시 넣기
				continue
			}

			delivery.Ack(false) // 저장 성공 → 큐에서 제거
			count++
			if count%1000 == 0 {
				console.Log("[queue-to-mongo] %s: %d건 저장 중...", queueName, count)
			}
		}
	}
}

// HandleQueueToMongo 는 CLI에서 호출하는 진입점이다.
func HandleQueueToMongo(args []string) {
	stopHour := 16 // 기본 KST 16시 종료
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &stopHour)
	}
	RunQueueToMongo(stopHour)
}
