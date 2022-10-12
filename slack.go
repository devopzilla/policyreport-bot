package main

import (
	"fmt"
	"strings"

	"github.com/slack-go/slack"
)

func buildSlackMessage(filter Filter, findings []Finding) slack.MsgOption {
	blocks := []slack.Block{}

	headerString := fmt.Sprintf("Top %d findings of the day 🌞", filter.Limit)
	if len(findings) == 0 {
		headerString = "Found nothing 🏜️ this could be good news 🤞"
	}
	headerText := slack.NewTextBlockObject(
		"plain_text",
		headerString,
		true,
		false,
	)
	header := slack.NewHeaderBlock(headerText)
	blocks = append(blocks, header)

	labels := []string{}
	for l, v := range filter.Labels {
		labels = append(labels, fmt.Sprintf("%s=%s", l, v))
	}
	contextText := slack.NewTextBlockObject(
		"plain_text",
		fmt.Sprintf("Filter: %s", strings.Join(labels, ",")),
		true,
		false,
	)
	context := slack.NewContextBlock("context", contextText)
	blocks = append(blocks, context)

	for i, finding := range findings {
		if i > 0 {
			blocks = append(blocks, slack.NewDividerBlock())
		}
		text := slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf(
				"*[%d]* `%s` in %s *%s/%s*\n```%s```\n",
				i+1,
				finding.Policy,
				strings.ToLower(finding.Resource.Kind),
				finding.Resource.Namespace,
				finding.Resource.Name,
				finding.Message,
			),
			false,
			false,
		)
		if finding.Properties != nil {
			for k, v := range finding.Properties {
				text.Text += fmt.Sprintf("*%s:* %s\n", k, v)
			}
		}

		text.Text += fmt.Sprintf(
			"```kubectl get -n %s %s/%s```",
			finding.Resource.Namespace,
			strings.ToLower(finding.Resource.Kind), finding.Resource.Name,
		)
		section := slack.NewSectionBlock(text, []*slack.TextBlockObject{}, nil)
		blocks = append(blocks, section)
	}

	return slack.MsgOptionBlocks(blocks...)
}
