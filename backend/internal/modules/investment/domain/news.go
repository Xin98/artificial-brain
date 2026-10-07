package domain

import (
	"sort"
	"strings"
	"time"
)

const TopicMappingVersion = "keywords-v1"

type Topic struct {
	Name                                      string
	MappingVersion                            string
	Count7Days, Count24Hours, Previous24Hours int
	Change24Hours                             int
	Articles                                  []NewsItem
}

func AggregateTopics(items []NewsItem, asOf time.Time) ([]Topic, error) {
	if asOf.IsZero() {
		return nil, ErrInvalidInput
	}
	groups := map[string]*Topic{}
	seen := map[string]bool{}
	rules := []struct {
		name     string
		keywords []string
	}{{"earnings", []string{"earnings", "quarterly results", "财报", "业绩"}}, {"merger", []string{"merger", "acquisition", "takeover", "并购", "收购"}}, {"product", []string{"product", "launch", "新品", "产品"}}, {"regulation", []string{"regulator", "antitrust", "lawsuit", "监管", "诉讼"}}, {"macro", []string{"inflation", "interest rate", "federal reserve", "通胀", "利率"}}}
	sorted := append([]NewsItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].AvailableAt.Before(sorted[j].AvailableAt) })
	for _, item := range sorted {
		if item.ID == "" || seen[item.ID] || item.AvailableAt.After(asOf) || item.PublishedAt.After(asOf) || item.PublishedAt.Before(asOf.Add(-7*24*time.Hour)) {
			continue
		}
		seen[item.ID] = true
		text := strings.ToLower(item.Title + " " + item.Summary)
		topic := "other"
		for _, rule := range rules {
			match := false
			for _, keyword := range rule.keywords {
				if strings.Contains(text, keyword) {
					match = true
					break
				}
			}
			if match {
				topic = rule.name
				break
			}
		}
		if groups[topic] == nil {
			groups[topic] = &Topic{Name: topic, MappingVersion: TopicMappingVersion, Articles: []NewsItem{}}
		}
		g := groups[topic]
		g.Count7Days++
		g.Articles = append(g.Articles, item)
		if !item.PublishedAt.Before(asOf.Add(-24 * time.Hour)) {
			g.Count24Hours++
		} else if !item.PublishedAt.Before(asOf.Add(-48 * time.Hour)) {
			g.Previous24Hours++
		}
	}
	out := []Topic{}
	for _, g := range groups {
		g.Change24Hours = g.Count24Hours - g.Previous24Hours
		sort.Slice(g.Articles, func(i, j int) bool {
			if !g.Articles[i].PublishedAt.Equal(g.Articles[j].PublishedAt) {
				return g.Articles[i].PublishedAt.After(g.Articles[j].PublishedAt)
			}
			return g.Articles[i].ID < g.Articles[j].ID
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count7Days != out[j].Count7Days {
			return out[i].Count7Days > out[j].Count7Days
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
