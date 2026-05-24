package console

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
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
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		log.Fatalf("err : %s", err)
		return "", err
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

func GetKey(env EnvType) string {
	base64Str := env.FKEY[:len(env.FKEY)-1] + "="
	decodKye, _ := base64.StdEncoding.DecodeString(base64Str)
	return string(decodKye)
}

func GetDecode(firstEncode, mainKey string) string {
	firstDecode, _ := firstDecode(firstEncode, mainKey)
	finalDecode, _ := secondDecode(firstDecode)
	return finalDecode
}

func GetEncode(s string, mainKey string) string {
	firstEncode, _ := firstEncode(s)
	finalPw, _ := secondEncode(firstEncode, mainKey)
	return finalPw
}
