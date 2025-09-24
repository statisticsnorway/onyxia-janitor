package notify

import (
	"context"
	"fmt"
	"log/slog"
	"onyxia-janitor/pkg"
	"strings"

	"onyxia-janitor/pkg/onyxia"
	"onyxia-janitor/pkg/teamapi"
	"onyxia-janitor/pkg/template"
)

// ServicesAndUserInfo is provided as the context for the email templates.
type ServicesAndUserInfo struct {
	UserInfo teamapi.UserInfo
	Services []onyxia.ServiceWithRelease
	Username string
}

type EmailTemplate = template.AnonymousTemplate[ServicesAndUserInfo]

type EmailNotifier struct {
	userServices map[string][]onyxia.ServiceWithRelease

	subjectTemplate EmailTemplate
	bodyTemplate    EmailTemplate

	emailSender    EmailSender
	userInfoGetter UserInfoGetter

	result *pkg.ServiceWithReleaseActionResult
}

type EmailSender interface {
	SendEmail(user, subject, body string) error
}

type UserInfoGetter interface {
	GetUser(userPrincipalName string) (*teamapi.UserInfo, error)
}

type optFunc func(*EmailNotifier)

func New(
	sender EmailSender,
	userGetter UserInfoGetter,
	subjectTemplate EmailTemplate,
	bodyTemplate EmailTemplate,
	opts ...optFunc,
) *EmailNotifier {
	notifier := &EmailNotifier{
		emailSender:     sender,
		userInfoGetter:  userGetter,
		subjectTemplate: subjectTemplate,
		bodyTemplate:    bodyTemplate,
		result: &pkg.ServiceWithReleaseActionResult{
			FailedItems:     make([]string, 0),
			FailedItemType:  pkg.User,
			SuccessfulCount: 0,
		},
	}

	notifier.userServices = make(map[string][]onyxia.ServiceWithRelease)

	for _, f := range opts {
		f(notifier)
	}

	return notifier
}

// Process handles one user service at a time. In this case we just want
// to accumulate all the user's services and process them all at once in Finish.
func (n *EmailNotifier) Process(_ context.Context, swr onyxia.ServiceWithRelease) error {
	log := swr.AddToLogger(slog.Default())

	user, err := getUsernameFromNamespace(swr.Service.Namespace)
	if err != nil {
		log.Error("could not deduce username from namespace", "err", err)
		n.result.FailedItems = append(n.result.FailedItems, swr.Service.Namespace)
		return err
	}

	n.userServices[user] = append(n.userServices[user], swr)
	return nil
}

// Finish sends emails to all the users about the services processed in Process
func (n *EmailNotifier) Finish(ctx context.Context) (*pkg.ServiceWithReleaseActionResult, error) {
	for user := range n.userServices {
		if err := n.notifyUser(ctx, user); err != nil {
			slog.Error("failed to notify user", "user", user)
			n.result.FailedItems = append(n.result.FailedItems, user)
		} else {
			slog.Info("successfully notified user", "user", user)
			n.result.SuccessfulCount++
		}
	}
	return n.result, nil
}

// notifyUser sends an email to one user about their old services.
func (n *EmailNotifier) notifyUser(_ context.Context, user string) error {
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

// getUsernameFromNamespace returns the username associated with
// the namespace. In our case we just trim the user-ssb- prefix.
// In the future this should be configurable.
//
// NOTE: Some accounts (users with an underscore in their name) will
// not work with this method. This is a problem with several applications
// in Dapla Lab atm.
func getUsernameFromNamespace(namespace string) (string, error) {
	if user := strings.TrimPrefix(namespace, "user-ssb-"); user != namespace {
		return user, nil
	}

	return "", fmt.Errorf("incorrect namespace format %q", namespace)
}
