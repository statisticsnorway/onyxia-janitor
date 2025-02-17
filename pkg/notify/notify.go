package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"onyxia-janitor/common"
	"onyxia-janitor/pkg/auth"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

var serviceNameRegex = regexp.MustCompile(`^([a-zA-Z-]+)`)

type emailNotifier struct {
	kubernetes    *kubernetes.Clientset
	settings      *cli.EnvSettings
	emailApiUrl   string
	senderEmail   string
	timeThreshold time.Duration
}

type optFunc func(*emailNotifier)

func WithSenderEmail(email string) optFunc {
	return func(n *emailNotifier) {
		n.senderEmail = email
	}
}

func WithTimeThreshold(threshold time.Duration) optFunc {
	return func(n *emailNotifier) {
		n.timeThreshold = threshold
	}
}

func New(client *kubernetes.Clientset, settings *cli.EnvSettings, emailApiUrl string, opts ...optFunc) *emailNotifier {
	notifier := &emailNotifier{
		kubernetes:    client,
		settings:      settings,
		emailApiUrl:   emailApiUrl,
		senderEmail:   "ikkesvar@ssb.no",
		timeThreshold: 7 * 24 * time.Hour,
	}

	for _, f := range opts {
		f(notifier)
	}

	return notifier
}

func (n *emailNotifier) NotifyUsersAboutOldServices(prefix string) error {
	log.Println("Checking for old Helm releases in user namespaces...")

	namespaces, err := common.GetNamespacesWithPrefix(n.kubernetes, prefix)
	if err != nil {
		return fmt.Errorf("error retrieving namespaces: %w", err)
	}

	for _, namespace := range namespaces {
		userEmail := getUserEmailFromNamespace(namespace)
		if userEmail == "" {
			log.Printf("Skipping namespace %s: Could not determine user email", namespace)
			continue
		}

		oldReleases, err := n.getOldHelmReleases(namespace)
		if err != nil {
			log.Printf("Error checking Helm releases in namespace %s: %v", namespace, err)
			continue
		}

		if len(oldReleases) > 0 {
			err = n.sendEmailNotification(userEmail, oldReleases)
			if err != nil {
				log.Printf("Error sending email to %s: %v", userEmail, err)
			} else {
				log.Printf("Email notification sent to %s for namespace %s", userEmail, namespace)
			}
		}
	}

	return nil
}

func getUserEmailFromNamespace(namespace string) string {
	if strings.HasPrefix(namespace, "user-ssb-") {
		username := strings.TrimPrefix(namespace, "user-ssb-")
		email := fmt.Sprintf("%s@ssb.no", strings.ToLower(username))
		log.Printf("Mapped namespace %s to email: %s", namespace, email)
		return email
	}

	log.Printf("Skipping namespace %s: Namespace format incorrect", namespace)
	return ""
}

func (n *emailNotifier) getOldHelmReleases(namespace string) ([]string, error) {
	actionConfig := new(action.Configuration)
	n.settings.SetNamespace(namespace)

	if err := actionConfig.Init(n.settings.RESTClientGetter(), namespace, "", log.Printf); err != nil {
		return nil, fmt.Errorf("error initializing Helm action config: %w", err)
	}

	listAction := action.NewList(actionConfig)
	listAction.AllNamespaces = false
	listAction.Deployed = true

	releases, err := listAction.Run()
	if err != nil {
		return nil, fmt.Errorf("error listing Helm releases in namespace %s: %w", namespace, err)
	}

	var oldReleases []string
	now := time.Now()

	for _, rel := range releases {
		if now.Sub(rel.Info.FirstDeployed.Time) > n.timeThreshold {
			oldReleases = append(oldReleases, rel.Name)
		}
	}

	return oldReleases, nil
}

func cleanServiceName(service string) string {
	matches := serviceNameRegex.FindStringSubmatch(service)
	if len(matches) > 1 {
		return strings.TrimSuffix(matches[1], "-")
	}
	return service
}

func (n *emailNotifier) sendEmailNotification(userEmail string, releases []string) error {
	if n.emailApiUrl == "" {
		log.Println("Email API URL is missing")
		return fmt.Errorf("missing email API URL (ONYXIA_JANITOR_EMAIL_API_URL)")
	}

	if len(userEmail) < 5 || !strings.Contains(userEmail, "@") {
		log.Printf("Skipping email notification: Invalid email format %s", userEmail)
		return nil
	}

	subject := "Dapla Lab: Du har tjenester som ble startet for mer enn 7 dager siden"

	releaseTable := "<table border='1' style='border-collapse: collapse;'><tr><th>Tjeneste</th></tr>"
	for _, release := range releases {
		releaseTable += fmt.Sprintf("<tr><td>%s</td></tr>", cleanServiceName(release))
	}
	releaseTable += "</table>"

	emailBody := fmt.Sprintf(`
		<html>
			<body>
				<p>Hei %s,</p>
				<p>Du har en eller flere tjenester (se tabell under) som ble startet for mer enn 7 dager siden.</p>
				<p>Vi anbefaler deg å slette tjenester som ble startet for mer enn 7 dager siden slik at du jobber på siste versjon av tjenesten.</p>

				%s <!-- Table of services -->

				<p>Husk å pushe kode til GitHub, og ta vare på andre filer som ligger i tjenestens filsystem, før du sletter.</p>

				<p>Vennlig hilsen,<br>Dapla Lab Team</p>
			</body>
		</html>`,
		strings.Split(userEmail, "@")[0],
		releaseTable,
	)

	emailPayload := map[string]string{
		"subject":      subject,
		"body":         emailBody,
		"from_name":    n.senderEmail,
		"content_type": "text/html",
	}

	payloadBytes, err := json.Marshal(map[string]interface{}{"email": emailPayload})
	if err != nil {
		return fmt.Errorf("error encoding email payload: %w", err)
	}

	token, err := auth.GetToken()
	if err != nil {
		log.Printf("Failed to obtain Keycloak token: %v", err)
		return err
	}

	url := fmt.Sprintf(n.emailApiUrl, userEmail)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		log.Printf("Skipping email for %s: User not found (404)", userEmail)
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("email API returned status: %d", resp.StatusCode)
	}

	log.Printf("Email notification successfully sent to %s", userEmail)
	return nil
}
