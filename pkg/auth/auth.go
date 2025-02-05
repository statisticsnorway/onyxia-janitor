package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const tokenURL = "https://auth.ssb.no/realms/ssb/protocol/openid-connect/token"

var cachedToken string
var tokenExpiry time.Time

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func GetToken() (string, error) {
	if time.Now().Before(tokenExpiry) {
		return cachedToken, nil
	}

	clientID := os.Getenv("ONYXIA_JANITOR_CLIENT_ID")
	clientSecret := os.Getenv("ONYXIA_JANITOR_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		log.Println("Keycloak client ID or secret is missing")
		return "", fmt.Errorf("missing Keycloak credentials")
	}

	data := []byte(fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s",
		clientID, clientSecret))

	req, err := http.NewRequest("POST", tokenURL, bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	cachedToken = tokenResp.AccessToken
	tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	log.Println("Successfully obtained Keycloak token")
	return cachedToken, nil
}
