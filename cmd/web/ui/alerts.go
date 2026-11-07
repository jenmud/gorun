package ui

// AlertsPageData is everything the alerts page shows.
type AlertsPageData struct {
	Rules      []AlertRuleRow
	Alerts     []AlertItem
	Stats      []TaskStatRow
	Flash      string
	FlashBad   bool
	HasHistory bool
}

// AlertRuleRow describes one rule from the Gorunfile.
type AlertRuleRow struct {
	Name      string
	Kind      string
	Target    string
	Threshold string
}

// AlertItem is one alert that fired, newest first.
type AlertItem struct {
	When    string
	Rule    string
	Target  string
	Message string
	RunID   string
}

// TaskStatRow summarises one task's recent history.
type TaskStatRow struct {
	Target      string
	Runs        string
	SuccessRate string
	Median      string
	P95         string
	Last        string
	LastFailed  bool
}
