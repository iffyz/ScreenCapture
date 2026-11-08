package main

// 腾讯云 TC3-HMAC-SHA256 签名与通用 HTTP 调用。
// 两个模式共用：只是 X-TC-Action 不同。

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmac256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

// buildAuth 组装 TC3-HMAC-SHA256 请求头。
func buildAuth(payload []byte, action string) (http.Header, error) {
	if action == "" {
		return nil, errors.New("action 为空")
	}
	const contentType = "application/json; charset=utf-8"
	ts := time.Now().Unix()
	date := time.Unix(ts, 0).UTC().Format("2006-01-02")

	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\n", contentType, apiHost)
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{
		"POST", "/", "", canonicalHeaders, signedHeaders, sha256Hex(payload),
	}, "\n")

	scope := date + "/" + apiService + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256",
		strconv.FormatInt(ts, 10),
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	kDate := hmac256([]byte("TC3"+cfg.SecretKey), date)
	kService := hmac256(kDate, apiService)
	kSigning := hmac256(kService, "tc3_request")
	signature := hex.EncodeToString(hmac256(kSigning, stringToSign))

	h := http.Header{}
	h.Set("Authorization", fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cfg.SecretID, scope, signedHeaders, signature))
	h.Set("Content-Type", contentType)
	h.Set("X-TC-Action", action)
	h.Set("X-TC-Version", apiVersion)
	h.Set("X-TC-Timestamp", strconv.FormatInt(ts, 10))
	if cfg.Region != "" {
		h.Set("X-TC-Region", cfg.Region)
	}
	return h, nil
}

// postOCR 发一次 OCR 请求，返回原始响应体。
// 网络类错误重试一次；接口层错误（鉴权、限流等）直接透传。
func postOCR(payload []byte, action string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	var lastErr error

	for attempt := 0; attempt < 2; attempt++ {
		header, err := buildAuth(payload, action)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequest(http.MethodPost, "https://"+apiHost, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		for k, v := range header {
			req.Header[k] = v
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == 0 {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("网络请求失败: %w", err)
		}

		body, readErr := readAll(resp)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取响应失败: %w", readErr)
		}
		return body, nil
	}
	return nil, fmt.Errorf("网络请求失败: %w", lastErr)
}

func readAll(resp *http.Response) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(resp.Body)
	return buf.Bytes(), err
}

// apiError 是腾讯云返回的 Response.Error。
type apiError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func (e *apiError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}
