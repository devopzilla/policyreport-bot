package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/kyverno/kyverno/pkg/client/clientset/versioned"

	"github.com/slack-go/slack"
)

type FindingResource struct {
	Kind      string
	Namespace string
	Name      string
	Labels    map[string]string
}

type Finding struct {
	Source     string
	Severity   string
	Policy     string
	Message    string
	Properties map[string]string
	Resource   *FindingResource
}

type Filter struct {
	Channel *string           `yaml:"channel"`
	Labels  map[string]string `yaml:"labels"`
	Limit   uint              `yaml:"limit"`
}

func main() {
	CONFIG_PATH := os.Getenv("CONFIG_PATH")
	if CONFIG_PATH == "" {
		CONFIG_PATH = "./filters.yaml"
		// log.Fatalf("Missing env var CONFIG_PATH.")
	}

	SLACK_TOKEN := os.Getenv("SLACK_TOKEN")
	if SLACK_TOKEN == "" {
		log.Fatalf("Missing env var SLACK_TOKEN.")
	}

	slackAPI := slack.New(SLACK_TOKEN)

	// var kubeconfig *string
	// if home := homedir.HomeDir(); home != "" {
	// 	kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	// } else {
	// 	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	// }
	// flag.Parse()

	// config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	// if err != nil {
	// 	panic(err.Error())
	// }

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Unable to create clientset %v", err)
	}

	polrClientSet, err := versioned.NewForConfig(config)
	if err != nil {
		log.Fatalf("Unable to create polr clientset %v", err)
	}

	policyreports, err := polrClientSet.Wgpolicyk8sV1alpha2().PolicyReports("").List(
		context.TODO(),
		metav1.ListOptions{},
	)
	if err != nil {
		log.Fatalf("Failed to fetch policy reports %v", err)
	}

	resourceCache := map[string]*FindingResource{}
	findings := []Finding{}

	for _, polr := range policyreports.Items {
		for _, result := range polr.GetResults() {
			if result.Result == "fail" && (result.Severity == "high" || result.Severity == "critical") {
				r := result.Resources[0]
				rkey := fmt.Sprintf("%s%s%s", r.Kind, r.Namespace, r.Name)

				var resource *FindingResource
				if cached, ok := resourceCache[rkey]; ok {
					resource = cached
				} else {
					resource = getFindingResource(clientSet, r.Kind, r.Namespace, r.Name)
					if resource == nil {
						log.Printf("Skipping resource %s %s %s couldn't fetch\n", r.Kind, r.Namespace, r.Name)
					}
					resourceCache[rkey] = resource
				}

				finding := Finding{
					Source:     result.Source,
					Severity:   string(result.Severity),
					Policy:     result.Policy,
					Message:    result.Message,
					Properties: result.Properties,
					Resource:   resource,
				}

				findings = append(findings, finding)
			}
		}
	}

	filters := parseConfig(CONFIG_PATH)

	for _, filter := range filters {
		if filter.Channel == nil {
			continue
		}
		filterFindings := []Finding{}
		for _, finding := range findings {
			include := true
			for label, value := range filter.Labels {
				findingLabelValue, ok := finding.Resource.Labels[label]
				include = include && (ok && findingLabelValue == value)
			}
			if include {
				filterFindings = append(filterFindings, finding)
			}
		}

		var msg slack.MsgOption
		if len(filterFindings) < int(filter.Limit) {
			msg = buildSlackMessage(filter, filterFindings)
		} else {
			msg = buildSlackMessage(filter, filterFindings[:filter.Limit])
		}
		_, _, _, err := slackAPI.SendMessage(*filter.Channel, msg)
		if err != nil {
			log.Fatalf("Unable to send slack message %v", err)
		}
	}
}

func getFindingResource(clientset *kubernetes.Clientset, kind string, namespace string, name string) *FindingResource {
	if kind == "ReplicaSet" {
		rs, err := clientset.AppsV1().ReplicaSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Unable to fetch resource %v", err)
		}

		ownerRefs := rs.GetOwnerReferences()
		if len(ownerRefs) > 0 && ownerRefs[0].Kind == "Deployment" {
			return getFindingResource(clientset, ownerRefs[0].Kind, namespace, ownerRefs[0].Name)
		}

		return &FindingResource{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Labels:    rs.GetLabels(),
		}
	}

	if kind == "StatefulSet" {
		s, err := clientset.AppsV1().StatefulSets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Unable to fetch resource %v", err)
		}

		return &FindingResource{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Labels:    s.GetLabels(),
		}
	}

	if kind == "Job" {
		j, err := clientset.BatchV1().Jobs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Unable to fetch resource %v", err)
		}

		ownerRefs := j.GetOwnerReferences()
		if len(ownerRefs) > 0 && ownerRefs[0].Kind == "CronJob" {
			return getFindingResource(clientset, ownerRefs[0].Kind, namespace, ownerRefs[0].Name)
		}
		return &FindingResource{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Labels:    j.GetLabels(),
		}
	}

	if kind == "CronJob" {
		c, err := clientset.BatchV1().CronJobs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Unable to fetch resource %v", err)
		}

		return &FindingResource{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Labels:    c.GetLabels(),
		}
	}

	if kind == "Deployment" {
		d, err := clientset.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("Unable to fetch resource %v", err)
		}

		return &FindingResource{
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Labels:    d.GetLabels(),
		}
	}

	return nil
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "  ")
	return string(s)
}
