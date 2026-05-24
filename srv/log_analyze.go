package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"MakeSQL/console"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// LogAnalyzeResult 는 로그 분석 결과
type LogAnalyzeResult struct {
	Range struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Hours int    `json:"hours"`
	} `json:"range"`
	Summary struct {
		TotalLogs      int    `json:"total_logs"`
		APIFailures    int    `json:"api_failures"`
		Errors         int    `json:"errors"`
		APIFailureRate string `json:"api_failure_rate"`
	} `json:"summary"`
	Modules        map[string]int            `json:"modules"`
	APIFailures    LogAnalyzeAPIFailures     `json:"api_failures"`
	ErrorLogs      []string                  `json:"error_logs"`
	KISCompletions []string                  `json:"kis_completions"`
	KISScheduler   KISSchedulerStatus        `json:"kis_scheduler"`
	News           LogAnalyzeNews            `json:"news"`
	AIAnalysis     struct{ Total int }       `json:"ai_analysis"`
}

type KISSchedulerStatus struct {
	PID       string            `json:"pid"`
	Completed []string          `json:"completed"`
	Running   []string          `json:"running"`
	Failed    []string          `json:"failed"`
}

type LogAnalyzeAPIFailures struct {
	Total    int            `json:"total"`
	ByTask   map[string]int `json:"by_task"`
	ByReason struct {
		HTTP500 int `json:"http500"`
		Timeout int `json:"timeout"`
		Other   int `json:"other"`
	} `json:"by_reason"`
}

type LogAnalyzeNews struct {
	Total   int      `json:"total"`
	Details []string `json:"details"`
}

var (
	reCategoryTag    = regexp.MustCompile(`\[([^\]]+)\]\[([^\]]+)\]`)
	reKISTask        = regexp.MustCompile(`\[([A-Z]{2}\.[A-Z_a-z]+)\]`)
	reSchedulerPID   = regexp.MustCompile(`시작 \(PID (\d+)\)`)
	reSchedulerDone  = regexp.MustCompile(`✓ (\S+) 완료`)
	reSchedulerRun   = regexp.MustCompile(`▶ (.+)`)
	reSchedulerFail  = regexp.MustCompile(`\[(\S+)\] 명령 \d+ 실패: (.+)`)
)

// HandleLogAnalyze 는 log-analyze 서브커맨드를 처리합니다
// ./abledb log-analyze [연결이름] [시간(h)]
func HandleLogAnalyze(ctx context.Context, cfg *console.Config, args []string) {
	if len(args) < 1 {
		fmt.Println("사용법: ./abledb log-analyze [연결이름] [시간(h)]")
		fmt.Println("예: ./abledb log-analyze white 3")
		os.Exit(1)
	}

	connName := args[0]
	hours := 3
	if len(args) >= 2 {
		if h, err := strconv.Atoi(args[1]); err == nil {
			hours = h
		}
	}

	// MongoDB 연결
	uri, err := console.BuildMongoURI(cfg, connName)
	if err != nil {
		log.Fatalf("%v", err)
	}
	client, err := console.ConnectMongo(ctx, uri,
		cfg.MongoDB.Options.ConnectTimeoutMS,
		120000, // socket timeout 2분
	)
	if err != nil {
		log.Fatalf("[%s] 연결 실패: %v", connName, err)
	}
	defer client.Disconnect(ctx)

	// KST 시간 계산
	kstNow := time.Now().UTC().Add(9 * time.Hour)
	kstFrom := kstNow.Add(-time.Duration(hours) * time.Hour)
	fromTime := kstFrom.Format("2006/01/02 15:04:05")
	toTime := kstNow.Format("2006/01/02 15:04:05")
	colFrom := kstFrom.Format("2006/01/02")
	colTo := kstNow.Format("2006/01/02")

	logDB := client.Database("LOG")

	// 대상 컬렉션 결정
	type colTarget struct {
		name   string
		filter bson.M
	}
	var targets []colTarget

	if colFrom != colTo {
		targets = append(targets, colTarget{colFrom, bson.M{"Time": bson.M{"$gte": fromTime}}})
		targets = append(targets, colTarget{colTo, bson.M{"Time": bson.M{"$lte": toTime}}})
	} else {
		targets = append(targets, colTarget{colTo, bson.M{"Time": bson.M{"$gte": fromTime, "$lte": toTime}}})
	}

	// 집계 변수
	modules := map[string]int{}
	apiFailByTask := map[string]int{}
	apiReasons := struct{ http500, timeout, other int }{}
	var errorLogs []string
	var kisCompletions []string
	var newsDetails []string
	totalLogs := 0
	apiFailTotal := 0
	errorTotal := 0
	aiCount := 0

	// KIS 스케줄러 상태
	var schedulerPID string
	schedulerCompleted := map[string]bool{}
	schedulerRunning := map[string]string{}
	schedulerFailed := map[string]string{}

	// 각 컬렉션에서 cursor 스트리밍 (projection으로 Msg, Time만)
	for _, t := range targets {
		col := logDB.Collection(t.name)

		// Time 인덱스 확보 (없으면 생성)
		col.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: bson.D{{Key: "Time", Value: 1}},
		})

		opts := options.Find().
			SetProjection(bson.M{"Time": 1, "Msg": 1, "_id": 0}).
			SetSort(bson.D{{Key: "Time", Value: 1}})

		cursor, err := col.Find(ctx, t.filter, opts)
		if err != nil {
			log.Fatalf("[%s] 쿼리 실패: %v", t.name, err)
		}

		for cursor.Next(ctx) {
			var doc struct {
				Time string `bson:"Time"`
				Msg  string `bson:"Msg"`
			}
			if err := cursor.Decode(&doc); err != nil {
				continue
			}
			totalLogs++
			msg := doc.Msg

			// 모듈별 집계
			if m := reCategoryTag.FindStringSubmatch(msg); len(m) == 3 {
				key := m[1] + "/" + m[2]
				modules[key]++
			}

			// API 실패
			if strings.Contains(msg, "API 실패 [") {
				apiFailTotal++
				if m := reKISTask.FindStringSubmatch(msg); len(m) == 2 {
					apiFailByTask[m[1]]++
				}
				if strings.Contains(msg, "500") {
					apiReasons.http500++
				} else if strings.Contains(msg, "deadline") || strings.Contains(msg, "Timeout") {
					apiReasons.timeout++
				} else {
					apiReasons.other++
				}
			}

			// ERROR/WARN
			if strings.Contains(msg, "[ERROR]") || strings.Contains(msg, "[WARN]") {
				errorTotal++
				if len(errorLogs) < 20 {
					truncMsg := msg
					if len(truncMsg) > 200 {
						truncMsg = truncMsg[:200]
					}
					errorLogs = append(errorLogs, doc.Time+" "+truncMsg)
				}
			}

			// KIS 완료
			if strings.Contains(msg, "[KIS][pipeline]") && strings.Contains(msg, "완료:") {
				if len(kisCompletions) < 30 {
					truncMsg := msg
					if len(truncMsg) > 150 {
						truncMsg = truncMsg[:150]
					}
					kisCompletions = append(kisCompletions, truncMsg)
				}
			}

			// 뉴스
			if strings.Contains(msg, "DB 저장 완료:") || strings.Contains(msg, "본문 채우기 성공") || strings.Contains(msg, "목록 크롤링 완료") {
				truncMsg := msg
				if len(truncMsg) > 150 {
					truncMsg = truncMsg[:150]
				}
				newsDetails = append(newsDetails, doc.Time+" "+truncMsg)
				if len(newsDetails) > 5 {
					newsDetails = newsDetails[len(newsDetails)-5:]
				}
			}

			// AI 분석
			if strings.Contains(msg, "[clProcessor]") || strings.Contains(msg, "[TopReason]") || strings.Contains(msg, "[StockTopReason]") {
				aiCount++
			}

			// KIS 스케줄러 상태 추적
			if strings.Contains(msg, "[KIS][scheduler]") {
				if m := reSchedulerPID.FindStringSubmatch(msg); len(m) == 2 {
					schedulerPID = m[1]
				}
				if m := reSchedulerDone.FindStringSubmatch(msg); len(m) == 2 {
					schedulerCompleted[m[1]] = true
					delete(schedulerRunning, m[1])
				}
				if m := reSchedulerRun.FindStringSubmatch(msg); len(m) == 2 {
					// "Phase N: 설명 → task_name (cmd)" 에서 task_name 추출
					runDesc := m[1]
					parts := strings.Split(runDesc, " → ")
					taskKey := runDesc
					if len(parts) == 2 {
						// "task_name (cmd)" 에서 task_name
						tn := strings.Split(parts[1], " (")
						taskKey = tn[0]
					}
					schedulerRunning[taskKey] = runDesc
				}
				if m := reSchedulerFail.FindStringSubmatch(msg); len(m) == 3 {
					schedulerFailed[m[1]] = m[2]
				}
			}
		}
		cursor.Close(ctx)
	}

	// 결과 구조체 생성
	result := LogAnalyzeResult{}
	result.Range.From = fromTime
	result.Range.To = toTime
	result.Range.Hours = hours
	result.Summary.TotalLogs = totalLogs
	result.Summary.APIFailures = apiFailTotal
	result.Summary.Errors = errorTotal
	if totalLogs > 0 {
		result.Summary.APIFailureRate = fmt.Sprintf("%.2f%%", float64(apiFailTotal)/float64(totalLogs)*100)
	} else {
		result.Summary.APIFailureRate = "0%"
	}

	// 모듈별 정렬 (건수 내림차순)
	result.Modules = modules

	result.APIFailures.Total = apiFailTotal
	result.APIFailures.ByTask = apiFailByTask
	result.APIFailures.ByReason.HTTP500 = apiReasons.http500
	result.APIFailures.ByReason.Timeout = apiReasons.timeout
	result.APIFailures.ByReason.Other = apiReasons.other

	result.ErrorLogs = errorLogs
	result.KISCompletions = kisCompletions

	// KIS 스케줄러 상태
	result.KISScheduler.PID = schedulerPID
	for k := range schedulerCompleted {
		result.KISScheduler.Completed = append(result.KISScheduler.Completed, k)
	}
	sort.Strings(result.KISScheduler.Completed)
	for k := range schedulerRunning {
		// running에 있지만 completed에도 있으면 이미 완료된 것
		if !schedulerCompleted[k] {
			result.KISScheduler.Running = append(result.KISScheduler.Running, k)
		}
	}
	sort.Strings(result.KISScheduler.Running)
	for k, reason := range schedulerFailed {
		result.KISScheduler.Failed = append(result.KISScheduler.Failed, k+": "+reason)
	}
	sort.Strings(result.KISScheduler.Failed)

	result.News.Total = modules["News/src"] + modules["News/News"]
	result.News.Details = newsDetails
	result.AIAnalysis.Total = aiCount

	// JSON 출력 (정렬된 모듈 포함)
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	// stderr에 요약
	fmt.Fprintf(os.Stderr, "[log-analyze] %s %dh: %d logs, %d failures, %d errors\n",
		connName, hours, totalLogs, apiFailTotal, errorTotal)

	// 모듈별 요약 (stderr)
	type modCount struct {
		name  string
		count int
	}
	var sorted []modCount
	for k, v := range modules {
		sorted = append(sorted, modCount{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })
	fmt.Fprintf(os.Stderr, "[log-analyze] 모듈별 TOP:\n")
	for i, m := range sorted {
		if i >= 10 {
			break
		}
		fmt.Fprintf(os.Stderr, "  %s: %d\n", m.name, m.count)
	}
}
