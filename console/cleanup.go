package console

import "fmt"

// Cleanup 은 모든 리소스를 정리합니다.
// main()에서 defer console.Cleanup()으로 호출하세요.
func Cleanup() {
	// 패닉 복구
	if r := recover(); r != nil {
		fmt.Printf("%s [Cleanup] 프로그램 패닉 복구: %v\n", GenerateTimestampedString(), r)
	}

	Log("=== 리소스 정리 시작 ===")

	// Step 1: 로거 배치 프로세서 종료 (먼저 종료하여 로그 유실 방지)
	stopLogger()
	StopZap() // Zap logger 정리

	// Step 2: RabbitMQ 정리 (로거 종료 후 정리, 콘솔 출력만)
	if RabbitMQSession != nil {
		for name, ch := range RabbitMQSession.Channels {
			if ch != nil {
				if err := ch.Close(); err != nil {
					fmt.Printf("%s [Cleanup] 채널 %s 닫기 실패: %v\n", GenerateTimestampedString(), name, err)
				} else {
					fmt.Printf("%s [Cleanup] 채널 %s 닫기 완료\n", GenerateTimestampedString(), name)
				}
			}
		}

		if RabbitMQSession.Conn != nil && !RabbitMQSession.Conn.IsClosed() {
			if err := RabbitMQSession.Conn.Close(); err != nil {
				fmt.Printf("%s [Cleanup] RabbitMQ 연결 닫기 실패: %v\n", GenerateTimestampedString(), err)
			} else {
				fmt.Printf("%s [Cleanup] RabbitMQ 연결 닫기 완료\n", GenerateTimestampedString())
			}
		}
	}

	// Step 3: MSSQL 정리
	if MsConn != nil {
		if err := MsConn.Close(); err != nil {
			fmt.Printf("%s [Cleanup] MSSQL 연결 종료 실패: %v\n", GenerateTimestampedString(), err)
		}
	}

	fmt.Printf("%s === 리소스 정리 완료 ===\n", GenerateTimestampedString())
}
