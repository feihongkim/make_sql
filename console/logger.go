package console

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel 로그 레벨 타입
type LogLevel int

const (
	// DEBUG 모든 로그 출력 (기본값)
	DEBUG LogLevel = 0
	// INFO INFO, ERROR 로그만 출력
	INFO LogLevel = 1
	// ERROR ERROR 로그만 출력
	ERROR LogLevel = 2
	// TEST ERROR + 테스트 관련 로그만 출력 (API 호출, MERGE, Pipeline)
	TEST LogLevel = 3
)

var (
	LogEnabled      bool     = true
	DebugEnabled    bool     = false
	CurrentLogLevel LogLevel = DEBUG // 기본값: DEBUG (모든 로그)

	// RabbitMQ 배치 처리용 변수
	logQueue     chan string
	logBatchSize = 100
	logTimeout   = 100 * time.Millisecond
	logShutdown  chan struct{}
	logWg        sync.WaitGroup
)

// getProjectName 설정된 프로젝트명 반환
func getProjectName() string {
	if Env.LOGGER_PROJECT_NAME != "" {
		return Env.LOGGER_PROJECT_NAME
	}
	return "KIS"
}

// Log 일반 정보 로그 (INFO 레벨)
func Log(format string, v ...interface{}) {
	logWithLevel("INFO", format, v...)
}

// LogError 에러 로그 (ERROR 레벨)
func LogError(format string, v ...interface{}) {
	logWithLevel("ERROR", format, v...)
}

// LogWarning 경고 로그 (WARNING 레벨)
func LogWarning(format string, v ...interface{}) {
	logWithLevel("WARNING", format, v...)
}

// LogDebug 디버그 로그 (DEBUG 레벨) - DebugEnabled=true일 때만 출력
func LogDebug(format string, v ...interface{}) {
	if !DebugEnabled {
		return
	}
	logWithLevel("DEBUG", format, v...)
}

// Tele Telegram 알림 로그 (INFO 레벨, Sender="KIS-tele")
// 중요한 이벤트를 Telegram으로 전송합니다.
// TELEGRAM_ENABLED=false이면 일반 Log()로 fallback
func Tele(format string, v ...interface{}) {
	// Feature flag 체크: Telegram이 비활성화되어 있으면 일반 로그로 전송
	if !Env.TELEGRAM_ENABLED {
		Log(format, v...)
		return
	}

	packageName := getCallerPackageName()
	prefix := GenerateTimestampedString()

	// 메시지 포맷팅 (logWithLevel()과 동일)
	var userMsg string
	if len(v) == 0 {
		userMsg = format
	} else {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PANIC] fmt.Sprintf failed in Tele(): %v\n", r)
				fmt.Printf("[PANIC] Format: %s, Args: %v\n", format, v)
			}
		}()
		userMsg = fmt.Sprintf(format, v...)
	}

	// 로그 레벨 필터링 체크 (INFO 레벨로 체크)
	if !shouldLog("INFO", userMsg) {
		return
	}

	// 콘솔 & RabbitMQ 메시지: 타임스탬프 [KIS][패키지명] [INFO] 메시지
	// (콘솔과 RabbitMQ 메시지 본문 모두 동일하게 [KIS] 사용)
	msg := fmt.Sprintf("%s [%s][%s] [%s] %s", prefix, getProjectName(), packageName, "INFO", userMsg)

	// 콘솔 출력
	if LogEnabled {
		fmt.Println(msg)
	}

	// RabbitMQ 전송용 JSON 구조체 (Sender="KIS-tele"로 Telegram 라우팅)
	logStruct := LogMessage{
		Sender: "KIS-tele", // ← 핵심: Telegram 라우팅용 Sender (Msg 내용과 독립)
		Time:   prefix,
		Msg:    msg, // ← RabbitMQ 메시지 본문에는 [KIS] 포함 (KIS-tele 아님!)
	}
	jsonBytes, err := json.Marshal(logStruct)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal log to JSON (Tele): %v\n", err)
		return
	}
	sendToLogQueue(string(jsonBytes))
}

// shouldLog 현재 로그 레벨에 따라 로그를 출력할지 결정
func shouldLog(level string, message string) bool {
	switch CurrentLogLevel {
	case DEBUG:
		// DEBUG: 모든 로그 출력
		return true
	case INFO:
		// INFO: INFO, ERROR만 출력
		return level == "INFO" || level == "ERROR"
	case ERROR:
		// ERROR: ERROR만 출력
		return level == "ERROR"
	case TEST:
		// TEST: ERROR + 테스트 관련 로그만 출력
		if level == "ERROR" {
			return true
		}
		// 테스트 관련 키워드 체크
		testKeywords := []string{
			"API", "calling", "complete",
			"MERGE", "INSERT", "성공", "실패",
			"Pipeline", "START", "COMPLETE",
			"Consumer", "처리",
			"msg_cd", "ERROR",
		}
		msgLower := strings.ToLower(message)
		for _, keyword := range testKeywords {
			if strings.Contains(msgLower, strings.ToLower(keyword)) ||
				strings.Contains(message, keyword) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// logWithLevel 로그 레벨과 함께 로그 출력
func logWithLevel(level string, format string, v ...interface{}) {
	packageName := getCallerPackageName()
	prefix := GenerateTimestampedString()

	// 메시지 포맷팅
	var userMsg string
	if len(v) == 0 {
		userMsg = format
	} else {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PANIC] fmt.Sprintf failed in logWithLevel(): %v\n", r)
				fmt.Printf("[PANIC] Format: %s, Args: %v\n", format, v)
			}
		}()
		userMsg = fmt.Sprintf(format, v...)
	}

	// 로그 레벨 필터링 체크
	if !shouldLog(level, userMsg) {
		return
	}

	// 전체 메시지 형식: 타임스탬프 [프로젝트명][패키지명] [레벨] 메시지
	fullMsg := fmt.Sprintf("%s [%s][%s] [%s] %s", prefix, getProjectName(), packageName, level, userMsg)

	// 콘솔 출력
	if LogEnabled {
		fmt.Println(fullMsg)
	}

	// RabbitMQ 전송용 JSON 구조체
	logStruct := LogMessage{
		Sender: getProjectName(),
		Time:   prefix,
		Msg:    fullMsg,
	}
	jsonBytes, err := json.Marshal(logStruct)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal log to JSON: %v\n", err)
		return
	}
	sendToLogQueue(string(jsonBytes))
}

// SetLogLevel 로그 레벨 설정 (문자열로 받아서 변환)
func SetLogLevel(levelStr string) {
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		CurrentLogLevel = DEBUG
	case "INFO":
		CurrentLogLevel = INFO
	case "ERROR":
		CurrentLogLevel = ERROR
	case "TEST":
		CurrentLogLevel = TEST
	default:
		fmt.Printf("[WARNING] Unknown log level '%s', using DEBUG\n", levelStr)
		CurrentLogLevel = DEBUG
	}
	fmt.Printf("[Init] Log level set to: %s\n", levelStr)
}

// LogImmediate 즉시 전송 로그 (배치 큐 우회)
func LogImmediate(format string, v ...interface{}) {
	packageName := getCallerPackageName()
	prefix := GenerateTimestampedString()

	var userMsg string
	if len(v) == 0 {
		userMsg = format
	} else {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PANIC] fmt.Sprintf failed in LogImmediate(): %v\n", r)
			}
		}()
		userMsg = fmt.Sprintf(format, v...)
	}

	fullMsg := fmt.Sprintf("%s [%s][%s] [INFO] %s", prefix, getProjectName(), packageName, userMsg)

	if LogEnabled {
		fmt.Println(fullMsg)
	}

	// RabbitMQ 즉시 전송 (배치 우회)
	logStruct := LogMessage{
		Sender: getProjectName(),
		Time:   prefix,
		Msg:    fullMsg,
	}
	jsonBytes, err := json.Marshal(logStruct)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal log to JSON (LogImmediate): %v\n", err)
		return
	}

	err = RabbitMQSession.Send("LOG", jsonBytes)
	if err != nil {
		fmt.Printf("%s [%s][logger] [ERROR] Immediate send failed: %v\n", prefix, getProjectName(), err)
	}
}

// Data 구조체 데이터를 JSON으로 직렬화하여 KISData 큐에 전송합니다
func Data(sender string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Panicf("Error marshalling JSON: %v", err)
	}
	stringData := string(jsonData)

	logVo := &LogMessage{
		Sender: sender,
		Time:   GenerateTimestampedString(),
		Msg:    stringData,
	}
	jsonData2, err := json.Marshal(logVo)
	if err != nil {
		log.Panicf("Error marshalling JSON: %v", err)
	}
	RabbitMQSession.Send("KISData", jsonData2)
	Log("Data sent, sender: %s", sender)
}

// PLog Panic 로그 - RabbitMQ 전송 후 panic 발생
func PLog(format string, v ...interface{}) {
	packageName := getCallerPackageName()
	prefix := GenerateTimestampedString()

	var userMsg string
	if len(v) == 0 {
		userMsg = format
	} else {
		userMsg = fmt.Sprintf(format, v...)
	}

	fullMsg := fmt.Sprintf("%s [%s][%s] [PANIC] %s", prefix, getProjectName(), packageName, userMsg)

	logStruct := LogMessage{
		Sender: getProjectName(),
		Time:   prefix,
		Msg:    fullMsg,
	}
	jsonBytes, err := json.Marshal(logStruct)
	if err != nil {
		fmt.Printf("Error marshaling log: %v\n", err)
	} else {
		sendToLogQueue(string(jsonBytes))
		time.Sleep(10 * time.Millisecond) // 배치 처리 대기
	}

	log.Panicf("[%s] [PANIC] %s\n", packageName, userMsg)
}

// getCallerPackageName 호출한 패키지명을 반환
func getCallerPackageName() string {
	_, file, _, ok := runtime.Caller(3) // 3단계 위 호출자 추적
	if !ok {
		return "unknown"
	}

	fileParts := strings.Split(filepath.ToSlash(file), "/")
	if len(fileParts) > 1 {
		pkgName := fileParts[len(fileParts)-2]

		if strings.HasSuffix(file, "/main.go") {
			return "main"
		}
		return pkgName
	}
	return "unknown"
}

// initLogger 배치 처리 시스템 초기화
func initLogger() {
	logQueue = make(chan string, 1000)
	logShutdown = make(chan struct{})
	logWg.Add(1)
	go processLogQueue()

	prefix := GenerateTimestampedString()
	fmt.Printf("%s [%s][logger] [INFO] 로그 배치 프로세서 시작 (size: %d, timeout: %v)\n",
		prefix, getProjectName(), logBatchSize, logTimeout)
}

// stopLogger 배치 처리 시스템 종료 (Graceful shutdown)
// Only close logShutdown — processLogQueue will drain logQueue and exit.
// Closing logQueue here would race with sendToLogQueue writes.
func stopLogger() {
	prefix := GenerateTimestampedString()
	fmt.Printf("%s [%s][logger] [INFO] 로그 배치 프로세서 종료 중...\n", prefix, getProjectName())

	close(logShutdown)
	logWg.Wait()

	prefix = GenerateTimestampedString()
	fmt.Printf("%s [%s][logger] [INFO] 로그 배치 프로세서 종료 완료\n", prefix, getProjectName())
}

// sendToLogQueue 로그 큐에 메시지 추가 (논블로킹)
func sendToLogQueue(message string) {
	select {
	case logQueue <- message:
		// 성공
	default:
		prefix := GenerateTimestampedString()
		fmt.Printf("%s [%s][logger] [WARNING] Log queue is full, dropping message\n", prefix, getProjectName())
	}
}

// processLogQueue 로그 배치 큐 처리 (백그라운드)
func processLogQueue() {
	defer logWg.Done()

	batch := make([]string, 0, logBatchSize)
	timer := time.NewTimer(logTimeout)
	defer timer.Stop()

	for {
		select {
		case msg, ok := <-logQueue:
			if !ok {
				if len(batch) > 0 {
					flushLogBatch(batch)
				}
				return
			}

			batch = append(batch, msg)

			if len(batch) >= logBatchSize {
				flushLogBatch(batch)
				batch = batch[:0]
				timer.Reset(logTimeout)
			}

		case <-timer.C:
			if len(batch) > 0 {
				flushLogBatch(batch)
				batch = batch[:0]
			}
			timer.Reset(logTimeout)

		case <-logShutdown:
			// Drain remaining messages from logQueue
			for {
				select {
				case msg := <-logQueue:
					batch = append(batch, msg)
				default:
					if len(batch) > 0 {
						flushLogBatch(batch)
					}
					return
				}
			}
		}
	}
}

// flushLogBatch RabbitMQ에 배치 저장 (재시도 포함)
func flushLogBatch(batch []string) {
	if len(batch) == 0 {
		return
	}

	maxRetries := 3
	retryDelay := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		var lastErr error
		successCount := 0
		for _, logMsg := range batch {
			err := RabbitMQSession.Send("LOG", []byte(logMsg))
			if err != nil {
				lastErr = err
			} else {
				successCount++
			}
		}

		if lastErr == nil {
			return
		}

		prefix := GenerateTimestampedString()
		fmt.Printf("%s [%s][logger] [ERROR] Batch send failed (attempt %d/%d, success: %d/%d): %v\n",
			prefix, getProjectName(), i+1, maxRetries, successCount, len(batch), lastErr)

		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}

	prefix := GenerateTimestampedString()
	fmt.Printf("%s [%s][logger] [ERROR] Failed to send batch after %d retries (lost %d messages)\n",
		prefix, getProjectName(), maxRetries, len(batch))
}
