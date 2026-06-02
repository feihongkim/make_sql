package srv

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	telegramBotToken = "8678638419:AAHqimHvEH1Lt6bXe1CFVWu5FzPDWelTuKQ"
	telegramChatID   = "7723743534"
)

// SendTelegramMsg 는 Telegram Bot API를 통해 메시지를 전송한다.
func SendTelegramMsg(text string) error {
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
