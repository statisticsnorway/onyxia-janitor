package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"onyxia-janitor/common"
	"onyxia-janitor/pkg/auth"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
)

const emailAPIURL = "https://dapla-team-api.intern.ssb.no/users/%s/messages"
const emailSender = "ikkesvar@ssb.no"
const timeThreshold = 7 * 24 * time.Hour

func NotifyUsersAboutOldServices(clientset *kubernetes.Clientset, helmSettings *cli.EnvSettings, prefix string) error {
	log.Println("Checking for old Helm releases in user namespaces...")

	namespaces, err := common.GetNamespacesWithPrefix(clientset, prefix)
	if err != nil {
		return fmt.Errorf("error retrieving namespaces: %w", err)
	}

	for _, namespace := range namespaces {
		userEmail := getUserEmailFromNamespace(namespace)
		if userEmail == "" {
			log.Printf("Skipping namespace %s: Could not determine user email", namespace)
			continue
		}

		oldReleases, err := getOldHelmReleases(namespace, helmSettings)
		if err != nil {
			log.Printf("Error checking Helm releases in namespace %s: %v", namespace, err)
			continue
		}

		if len(oldReleases) > 0 {
			err = sendEmailNotification(userEmail, oldReleases)
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

func getOldHelmReleases(namespace string, helmSettings *cli.EnvSettings) ([]string, error) {
	actionConfig := new(action.Configuration)
	helmSettings.SetNamespace(namespace)

	if err := actionConfig.Init(helmSettings.RESTClientGetter(), namespace, "", log.Printf); err != nil {
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
		if now.Sub(rel.Info.FirstDeployed.Time) > timeThreshold {
			oldReleases = append(oldReleases, rel.Name)
		}
	}

	return oldReleases, nil
}

func sendEmailNotification(userEmail string, releases []string) error {
	if len(userEmail) < 5 || !strings.Contains(userEmail, "@") {
		log.Printf("Skipping email notification: Invalid email format %s", userEmail)
		return nil
	}

	subject := fmt.Sprintf("Dapla Lab: Tjenesten %s har levd i over 7 dager.", releases[0])

	releaseList := ""
	for _, release := range releases {
		releaseList += fmt.Sprintf("- %s\n", release)
	}

	emailPayload := map[string]interface{}{
		"email": map[string]string{
			"subject": subject,
			"body": fmt.Sprintf(
				"Hei %s,\n\nDet er mer enn 7 dager siden tjenesten %s ble startet for første gang.\n\n"+
					"Vi anbefaler deg å slette tjenesten og starte en ny, slik at du jobber med en "+
					"oppdatert versjon. Husk å pushe kode til GitHub, og ta vare på andre filer som "+
					"ligger i tjenestens filsystem, før du sletter.\n",
				strings.Split(userEmail, "@")[0],
				releaseList,
			),
			"from_name": emailSender,
		},
	}

	payloadBytes, err := json.Marshal(emailPayload)
	if err != nil {
		return fmt.Errorf("error encoding email payload: %w", err)
	}

	token, err := auth.GetToken()
	if err != nil {
		log.Printf("Failed to obtain Keycloak token: %v", err)
		return err
	}

	url := fmt.Sprintf(emailAPIURL, userEmail)
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
