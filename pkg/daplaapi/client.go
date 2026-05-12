package daplaapi

import (
	"context"
	"fmt"
	"onyxia-janitor/pkg/action/notify"
	"time"

	"github.com/hasura/go-graphql-client"
	"golang.org/x/oauth2"
)

type Client struct {
	graphqlClient *graphql.Client
	apiUrl        string
}

type optFunc func(*Client)

func NewClient(apiUrl, serviceAccountToken string, opts ...optFunc) *Client {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: serviceAccountToken})
	httpClient := oauth2.NewClient(context.Background(), src)
	httpClient.Timeout = time.Second * 10

	graphqlClient := graphql.NewClient(apiUrl, httpClient)

	c := &Client{
		graphqlClient: graphqlClient,
		apiUrl:        apiUrl,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Client) SendEmail(userEmail, subject, body string) (string, error) {
	type SendMessageInput struct {
		Recipient string `json:"recipient"`
		Subject   string `json:"subject"`
		Message   string `json:"message"`
	}

	var sendMessageMutation struct {
		SendMessage struct {
			MessageID string `graphql:"messageId"`
		} `graphql:"sendMessage(input: $input)"`
	}

	variables := map[string]any{
		"input": SendMessageInput{
			Recipient: userEmail,
			Subject:   subject,
			Message:   body,
		},
	}

	err := c.graphqlClient.WithDebug(true).Mutate(context.Background(), &sendMessageMutation, variables)
	if err != nil {
		return "", fmt.Errorf("send email for dapla api failed %w", err)
	}
	return sendMessageMutation.SendMessage.MessageID, nil
}

func (c *Client) GetUser(userPrincipalEmail string) (*notify.UserInfo, error) {
	var userQuery struct {
		User struct {
			Name      graphql.String
			Email     graphql.String
			FirstName graphql.String
			LastName  graphql.String
		} `graphql:"user(email: $email)"`
	}
	variables := map[string]interface{}{
		"email": graphql.String(userPrincipalEmail),
	}

	err := c.graphqlClient.Query(context.Background(), &userQuery, variables)
	if err != nil {
		return nil, fmt.Errorf("query for dapla api failed %w", err)
	}

	return &notify.UserInfo{
		DisplayName: string(userQuery.User.Name),
		FirstName:   string(userQuery.User.FirstName),
		LastName:    string(userQuery.User.LastName),
		Email:       string(userQuery.User.Email),
	}, nil
}
