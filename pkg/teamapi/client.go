package teamapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2/clientcredentials"
)

type client struct {
	httpClient *http.Client
	teamApiUrl string

	senderEmail string
}

type optFunc func(*client)

func WithSenderEmail(email string) optFunc {
	return func(c *client) {
		c.senderEmail = email
	}
}

func NewClient(teamApiUrl, tokenUrl, clientId, clientSecret string, opts ...optFunc) *client {
	httpClient := (&clientcredentials.Config{
		ClientID:     clientId,
		ClientSecret: clientSecret,
		TokenURL:     tokenUrl,
	}).Client(context.Background())
	httpClient.Timeout = time.Second * 10

	c := &client{
		teamApiUrl:  teamApiUrl,
		senderEmail: "ikkesvar@ssb.no",
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

type teamApiMessageForUser struct {
	Email teamApiEmail `json:"email"`
}

type teamApiEmail struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	FromName string `json:"from_name"`
}

func (c *client) SendEmail(userEmail, subject, body string) error {
	endpoint := fmt.Sprintf("%s/users/%s/messages", c.teamApiUrl, userEmail)

	msg := teamApiMessageForUser{
		Email: teamApiEmail{
			Subject:  subject,
			Body:     body,
			FromName: c.senderEmail,
		},
	}
	msgJson, _ := json.Marshal(msg)

	res, err := c.httpClient.Post(
		endpoint,
		"application/json",
		bytes.NewReader(msgJson),
	)
	if err != nil {
		return fmt.Errorf("send email to %q: %w", userEmail, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("team api could not find user %q", userEmail)
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError:
		return fmt.Errorf("send email to %q, team api returned %q", userEmail, res.Status)
	default:
		return fmt.Errorf("send email to %q, team api returned unknown status %q", userEmail, res.Status)
	}
}
