package console

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}
	return string(b), nil
}

func firstEncode(s string) (string, error) {
	if len(s) < 3 {
		return "", fmt.Errorf("original string must be at least 3 characters")
	}

	rand5, err := randomString(5)
	if err != nil {
		return "", err
	}

	rand7, err := randomString(7)
	if err != nil {
		return "", err
	}

	part1 := s[:1]  // 첫 글자 (index 0)
	part2 := s[1:2] // 두 번째 글자 (index 1)
	part3 := s[2:]  // 나머지

	encoded := part1 + rand5 + part2 + rand7 + part3
	return encoded, nil
}

func secondEncode(plaintext, myKey string) (string, error) {
	key := []byte(myKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func firstDecode(encText, myKey string) (string, error) {
	key := []byte(myKey)
	ciphertext, err := base64.StdEncoding.DecodeString(encText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("암호문이 너무 짧습니다 (%d < nonce %d)", len(ciphertext), nonceSize)
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// 여기서 프로세스를 죽이지 않는다. 예전에는 log.Fatalf 였는데, 라이브러리
	// 함수가 os.Exit 을 부르면 config 한 줄이 어긋났을 때 스케줄러가 이유도
	// 남기지 않고 사라진다. 판단은 호출자(loadConfig)가 한다.
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("복호화 실패 (FKEY 가 맞는지 확인하세요): %w", err)
	}

	return string(plaintext), nil
}

func secondDecode(encoded string) (string, error) {
	if len(encoded) < 1+5+1+7 {
		return "", fmt.Errorf("encoded string too short")
	}

	part1 := encoded[:1]
	part2 := encoded[1+5 : 1+5+1]
	part3 := encoded[1+5+1+7:]

	original := part1 + part2 + part3

	if original[len(original)-1] == '_' {
		return original[:len(original)-1], nil
	}

	return original, nil
}

// GetKey 는 FKEY 에서 AES 키를 복원합니다.
func GetKey(env EnvType) (string, error) {
	if len(env.FKEY) < 2 {
		return "", fmt.Errorf("config.yaml 의 FKEY 가 비었거나 너무 짧습니다")
	}
	base64Str := env.FKEY[:len(env.FKEY)-1] + "="
	key, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return "", fmt.Errorf("FKEY 디코딩 실패: %w", err)
	}
	return string(key), nil
}

// GetDecode 는 암호화된 설정값을 복호화합니다.
//
// 에러를 삼키지 않는다. 예전에는 두 단계의 에러를 모두 버리고 빈 문자열을
// 돌려줬는데, 그러면 자격증명이 빈 값인 채로 기동해서 실패가 DB 연결 시점의
// 엉뚱한 메시지로 나타난다.
func GetDecode(encoded, mainKey string) (string, error) {
	decrypted, err := firstDecode(encoded, mainKey)
	if err != nil {
		return "", err
	}
	return secondDecode(decrypted)
}

func GetEncode(s string, mainKey string) string {
	firstEncode, _ := firstEncode(s)
	finalPw, _ := secondEncode(firstEncode, mainKey)
	return finalPw
}
