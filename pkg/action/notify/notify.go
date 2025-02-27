package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"onyxia-janitor/pkg/onyxia"
	"onyxia-janitor/pkg/teamapi"
	"onyxia-janitor/pkg/template"
)

type ServicesAndUserInfo struct {
	UserInfo teamapi.UserInfo
	Services []onyxia.ServiceWithRelease
	Username string
}

type EmailTemplate = template.AnonymousTemplate[ServicesAndUserInfo]

type emailNotifier struct {
	userServices map[string][]onyxia.ServiceWithRelease

	subjectTemplate EmailTemplate
	bodyTemplate    EmailTemplate

	emailSender    EmailSender
	userInfoGetter UserInfoGetter
}

type EmailSender interface {
	SendEmail(user, subject, body string) error
}

type UserInfoGetter interface {
	GetUser(userPrincipalName string) (*teamapi.UserInfo, error)
}

type optFunc func(*emailNotifier)

func New(
	sender EmailSender,
	userGetter UserInfoGetter,
	subjectTemplate EmailTemplate,
	bodyTemplate EmailTemplate,
	opts ...optFunc,
) *emailNotifier {
	notifier := &emailNotifier{
		emailSender:     sender,
		userInfoGetter:  userGetter,
		subjectTemplate: subjectTemplate,
		bodyTemplate:    bodyTemplate,
	}

	notifier.userServices = make(map[string][]onyxia.ServiceWithRelease)

	for _, f := range opts {
		f(notifier)
	}

	return notifier
}

func (n *emailNotifier) Process(ctx context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())

	user, err := getUsernameFromNamespace(swr.Service.Namespace)
	if err != nil {
		log.Error("could not deduce username from namespace", "err", err)
		return err
	}

	n.userServices[user] = append(n.userServices[user], swr)
	return nil
}

func (n *emailNotifier) Finish(ctx context.Context) error {
	for user := range n.userServices {
		if err := n.notifyUser(ctx, user); err != nil {
			slog.Error("failed to notify user", "user", user)
		} else {
			slog.Info("successfully notified user", "user", user)
		}
	}
	return nil
}

func (n *emailNotifier) notifyUser(ctx context.Context, user string) error {
	services, ok := n.userServices[user]
	if !ok {
		slog.Info("user has no registered old services, skipping", "user", user)
		return nil
	}
	principalEmail := fmt.Sprintf("%s@ssb.no", user)
	var ui *teamapi.UserInfo
	if n.userInfoGetter != nil {
		var err error
		ui, err = n.userInfoGetter.GetUser(principalEmail)
		if err != nil {
			slog.Error("error getting user info", "user", user, "err", err)
		}
	}
	if ui == nil {
		ui = &teamapi.UserInfo{DisplayName: user}
	}

	sui := ServicesAndUserInfo{Username: user, Services: services, UserInfo: *ui}
	subject, err := n.subjectTemplate.Execute(sui)
	if err != nil {
		slog.Error("failed to execute subject template", "err", err)
		return err
	}

	body, err := n.bodyTemplate.Execute(sui)
	if err != nil {
		slog.Error("failed to execute body template", "err", err)
		return err
	}

	if err := n.emailSender.SendEmail(principalEmail, subject, body); err != nil {
		slog.Error("failed to send notification", "user", principalEmail, "err", err)
		return err
	}

	slog.Info("successfully sent notification email", "user", principalEmail)
	return nil
}

func getUsernameFromNamespace(namespace string) (string, error) {
	if user := strings.TrimPrefix(namespace, "user-ssb-"); user != namespace {
		return user, nil
	}

	return "", fmt.Errorf("incorrect namespace format %q", namespace)
}
