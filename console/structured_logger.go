package console

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sync"
)

var (
	// ZapLogger 전역 zap logger 인스턴스
	ZapLogger *zap.Logger
	zapOnce   sync.Once
)

// initZap zap logger 초기화
func initZap() {
	zapOnce.Do(func() {
		config := zap.NewProductionConfig()
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		config.EncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}

		var err error
		ZapLogger, err = config.Build(
			zap.AddCaller(),
			zap.AddCallerSkip(1),
		)
		if err != nil {
			fmt.Printf("[ERROR] Failed to initialize zap logger: %v\n", err)
			// Fallback to no-op logger
			ZapLogger = zap.NewNop()
		}
	})
}

// getZapLogger zap logger 인스턴스 반환 (lazy initialization)
func getZapLogger() *zap.Logger {
	if ZapLogger == nil {
		initZap()
	}
	return ZapLogger
}

// LogInfo 구조화된 INFO 로그 (zap 사용)
// 사용 예: console.LogInfo("Processing stock", zap.String("stock_code", "005930"), zap.Int("count", 150))
func LogInfo(msg string, fields ...zap.Field) {
	logger := getZapLogger()
	logger.Info(msg, fields...)

	// 기존 RabbitMQ 전송 로직도 유지
	if LogEnabled {
		formattedMsg := formatZapLog("INFO", msg, fields)
		fmt.Println(formattedMsg)
	}
}

// LogWarn 구조화된 WARN 로그
func LogWarn(msg string, fields ...zap.Field) {
	logger := getZapLogger()
	logger.Warn(msg, fields...)

	if LogEnabled {
		formattedMsg := formatZapLog("WARN", msg, fields)
		fmt.Println(formattedMsg)
	}
}

// LogErr 구조화된 ERROR 로그 (기존 LogError와 구별)
func LogErr(msg string, fields ...zap.Field) {
	logger := getZapLogger()
	logger.Error(msg, fields...)

	if LogEnabled {
		formattedMsg := formatZapLog("ERROR", msg, fields)
		fmt.Println(formattedMsg)
	}
}

// LogDbg 구조화된 DEBUG 로그
func LogDbg(msg string, fields ...zap.Field) {
	if !DebugEnabled {
		return
	}

	logger := getZapLogger()
	logger.Debug(msg, fields...)

	if LogEnabled {
		formattedMsg := formatZapLog("DEBUG", msg, fields)
		fmt.Println(formattedMsg)
	}
}

// formatZapLog zap 필드를 포함한 로그 메시지 포맷팅
func formatZapLog(level string, msg string, fields []zap.Field) string {
	prefix := GenerateTimestampedString()
	packageName := getCallerPackageName()

	// 기본 메시지
	formatted := fmt.Sprintf("%s [%s][%s] [%s] %s",
		prefix, getProjectName(), packageName, level, msg)

	// 필드 추가
	if len(fields) > 0 {
		formatted += " {"
		for i, field := range fields {
			if i > 0 {
				formatted += ", "
			}
			formatted += fmt.Sprintf("%s: %v", field.Key, field.Interface)
		}
		formatted += "}"
	}

	return formatted
}

// LogWithFields 기존 Log() 함수와 호환되지만 구조화된 필드를 추가로 받음
// 사용 예: console.LogWithFields("Stock processed", map[string]interface{}{"stock_code": "005930", "price": 50000})
func LogWithFields(format string, fields map[string]interface{}, v ...interface{}) {
	// 기존 방식으로 메시지 포맷팅
	var userMsg string
	if len(v) == 0 {
		userMsg = format
	} else {
		userMsg = fmt.Sprintf(format, v...)
	}

	// zap 필드로 변환
	zapFields := make([]zap.Field, 0, len(fields))
	for k, val := range fields {
		switch v := val.(type) {
		case string:
			zapFields = append(zapFields, zap.String(k, v))
		case int:
			zapFields = append(zapFields, zap.Int(k, v))
		case int64:
			zapFields = append(zapFields, zap.Int64(k, v))
		case float64:
			zapFields = append(zapFields, zap.Float64(k, v))
		case bool:
			zapFields = append(zapFields, zap.Bool(k, v))
		case error:
			zapFields = append(zapFields, zap.Error(v))
		default:
			zapFields = append(zapFields, zap.Any(k, v))
		}
	}

	LogInfo(userMsg, zapFields...)
}

// StopZap zap logger 정리 (애플리케이션 종료 시 호출)
func StopZap() {
	if ZapLogger != nil {
		_ = ZapLogger.Sync()
	}
}
