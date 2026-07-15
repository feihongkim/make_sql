package srv

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	telegramBotToken = "8707979862:AAGufPfoD7M0h4L0E2Ct4JzFTJK-4mvz63s"
	telegramChatID   = "7723743534"
)

const telegramMaxLen = 4096

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
	apiURL := "https://api.telegram.org/bot" + telegramBotToken + "/sendMessage"
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {telegramChatID},
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
