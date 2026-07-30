package srv

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"MakeSQL/console"
)

const telegramMaxLen = 4096

// telegramCreds 는 config.yaml 에서 봇 토큰과 chat_id 를 읽는다.
//
// 예전에는 이 파일에 상수로 박혀 있었다. 소스는 저장소에 올라가므로 토큰이
// 그대로 공개됐다. 값이 비어 있으면 요청을 보내지 않고 에러를 돌려준다 —
// 잘못된 URL 로 POST 해서 404 를 원인 불명으로 만들지 않기 위해서다.
func telegramCreds() (token, chatID string, err error) {
	token = console.Env.TELEGRAM_BOT_TOKEN
	chatID = console.Env.TELEGRAM_CHAT_ID
	if token == "" || chatID == "" {
		return "", "", fmt.Errorf("config.yaml 에 TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID 가 없습니다")
	}
	return token, chatID, nil
}

// SendTelegramMsg 는 Telegram Bot API를 통해 메시지를 전송한다.
// 4096자 초과 시 분할 전송한다.
func SendTelegramMsg(text string) error {
	runes := []rune(text)
	for len(runes) > 0 {
		chunk := runes
		if len(chunk) > telegramMaxLen {
			chunk = runes[:telegramMaxLen]
		}
		runes = runes[len(chunk):]
		if err := sendChunk(string(chunk)); err != nil {
			return err
		}
	}
	return nil
}

func sendChunk(text string) error {
	token, chatID, err := telegramCreds()
	if err != nil {
		return err
	}
	apiURL := "https://api.telegram.org/bot" + token + "/sendMessage"
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {chatID},
		"text":    {text},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API 오류: %s", string(body))
	}
	return nil
}

// SendTelegramPhoto 는 이미지 파일을 Telegram 으로 전송한다.
// 차트처럼 그림이 결과물인 경우에 쓴다.
//
// sendPhoto 는 10MB 를 넘는 파일을 거부한다. 차트 PNG 는 보통 수백 KB 라
// 문제되지 않지만, 초과하면 API 가 오류를 돌려준다.
func SendTelegramPhoto(imagePath, caption string) error {
	token, chatID, err := telegramCreds()
	if err != nil {
		return err
	}
	f, err := os.Open(imagePath)
	if err != nil {
		return fmt.Errorf("이미지 열기 실패: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if caption != "" {
		if err := w.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := w.CreateFormFile("photo", filepath.Base(imagePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	apiURL := "https://api.telegram.org/bot" + token + "/sendPhoto"
	resp, err := http.Post(apiURL, w.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendPhoto 실패 (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}
