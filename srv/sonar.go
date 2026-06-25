package srv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"MakeSQL/console"
)

const sonarAPIURL = "https://api.perplexity.ai/chat/completions"

type sonarMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sonarRequest struct {
	Model    string         `json:"model"`
	Messages []sonarMessage `json:"messages"`
}

type sonarChoice struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

type sonarResponse struct {
	Choices   []sonarChoice `json:"choices"`
	Citations []string      `json:"citations"`
}

// HandleSonarSearch 는 Perplexity Sonar API로 검색하고 결과를 반환한다.
// model: "sonar", "sonar-pro", "sonar-reasoning-pro"
func HandleSonarSearch(model, query string) (string, error) {
	apiKey := getPerplexityKey()
	if apiKey == "" {
		return "", fmt.Errorf("Perplexity API 키를 찾을 수 없습니다")
	}

	reqBody := sonarRequest{
		Model: model,
		Messages: []sonarMessage{
			{Role: "system", Content: "한국어로 답변해주세요. 핵심 내용 위주로 간결하게 정리해주세요."},
			{Role: "user", Content: query},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("요청 생성 실패: %w", err)
	}

	req, err := http.NewRequest("POST", sonarAPIURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("HTTP 요청 생성 실패: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API 호출 실패: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("응답 읽기 실패: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 오류 (%d): %s", resp.StatusCode, string(respBody))
	}

	var result sonarResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("응답 파싱 실패: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("응답 내용 없음")
	}

	var sb strings.Builder
	sb.WriteString(result.Choices[0].Message.Content)

	if len(result.Citations) > 0 {
		sb.WriteString("\n\n📎 출처:")
		for i, url := range result.Citations {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("\n  %d. %s", i+1, url))
		}
	}

	return sb.String(), nil
}

func getPerplexityKey() string {
	if err := console.MsConn.ConnectMSSQL("white", "mykeys"); err != nil {
		console.LogError("[sonar] mykeys DB 연결 실패: %v", err)
		return ""
	}
	db, err := console.MsConn.GetDB("white:mykeys")
	if err != nil {
		console.LogError("[sonar] mykeys DB 가져오기 실패: %v", err)
		return ""
	}
	var key string
	err = db.QueryRow("SELECT VALUE_DATA FROM KeyValueStore WHERE GUBUN='PERPLEXITY' AND KEY_NAME='API_KEY'").Scan(&key)
	if err != nil {
		console.LogError("[sonar] API 키 조회 실패: %v", err)
		return ""
	}
	return key
}
