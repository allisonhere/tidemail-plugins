package main

import "strings"

// brand is a name scammers like to borrow. keywords are what shows up in a
// sender's display name (matched as whole words or phrases, lower case);
// domains are every domain the real company sends from. A subdomain of any of
// them counts as the company.
type brand struct {
	name     string
	keywords []string
	domains  []string
}

// brands is deliberately limited to companies that are both heavily
// impersonated and clear about the domains they send from. A keyword that is
// also an everyday word ("apple", "chase", "ups") is only listed as a phrase.
var brands = []brand{
	{"PayPal", []string{"paypal"}, []string{"paypal.com", "paypal.me", "paypal.de", "paypal.co.uk", "paypal.fr", "paypal.it", "paypal.es", "paypal.ca", "paypal.com.au", "paypal-communication.com", "paypal-mail.com"}},
	{"Apple", []string{"apple id", "apple support", "apple pay", "apple store", "icloud", "itunes", "app store"}, []string{"apple.com", "icloud.com", "me.com", "itunes.com", "appleid.com", "email.apple.com", "insideapple.apple.com"}},
	{"Microsoft", []string{"microsoft", "office 365", "outlook team", "onedrive", "xbox", "azure"}, []string{"microsoft.com", "microsoftonline.com", "office.com", "office365.com", "outlook.com", "live.com", "hotmail.com", "msn.com", "windows.com", "xbox.com", "azure.com", "sharepoint.com", "skype.com", "bing.com", "microsoftstoreemail.com", "accountprotection.microsoft.com"}},
	{"Google", []string{"google", "gmail team", "youtube"}, []string{"google.com", "gmail.com", "googlemail.com", "youtube.com", "withgoogle.com", "google.co.uk", "accounts.google.com", "googlegroups.com"}},
	{"Amazon", []string{"amazon", "amazon prime", "aws"}, []string{"amazon.com", "amazon.co.uk", "amazon.de", "amazon.ca", "amazon.fr", "amazon.es", "amazon.it", "amazon.in", "amazon.co.jp", "amazon.com.au", "amazonaws.com", "amazonses.com", "amazonpay.com", "primevideo.com", "audible.com", "twitch.tv", "amazonmusic.com", "amazon.jobs", "amazon.dev", "a2z.com", "amazon.science", "amazonpay.in"}},
	{"Netflix", []string{"netflix"}, []string{"netflix.com", "mailer.netflix.com", "account.netflix.com"}},
	{"Facebook", []string{"facebook", "instagram", "whatsapp", "meta platforms"}, []string{"facebook.com", "facebookmail.com", "instagram.com", "mail.instagram.com", "whatsapp.com", "meta.com", "fb.com", "messenger.com"}},
	{"LinkedIn", []string{"linkedin"}, []string{"linkedin.com", "e.linkedin.com", "bounce.linkedin.com"}},
	{"X (Twitter)", []string{"twitter"}, []string{"twitter.com", "x.com", "t.co"}},
	{"DocuSign", []string{"docusign"}, []string{"docusign.com", "docusign.net"}},
	{"Dropbox", []string{"dropbox"}, []string{"dropbox.com", "dropboxmail.com"}},
	{"Adobe", []string{"adobe"}, []string{"adobe.com", "adobesign.com", "email.adobe.com", "mail.adobe.com"}},
	{"Spotify", []string{"spotify"}, []string{"spotify.com"}},
	{"Zoom", []string{"zoom video", "zoom meeting", "zoom team"}, []string{"zoom.us", "zoom.com"}},
	{"Coinbase", []string{"coinbase"}, []string{"coinbase.com"}},
	{"Binance", []string{"binance"}, []string{"binance.com", "binance.us"}},
	{"Venmo", []string{"venmo"}, []string{"venmo.com"}},
	{"Zelle", []string{"zelle"}, []string{"zellepay.com", "zelle.com"}},
	{"Cash App", []string{"cash app", "cashapp"}, []string{"cash.app", "square.com", "squareup.com"}},
	{"Chase", []string{"chase bank", "jpmorgan", "chase online"}, []string{"chase.com", "jpmorgan.com", "jpmorganchase.com"}},
	{"Bank of America", []string{"bank of america", "bankofamerica"}, []string{"bankofamerica.com", "bofa.com", "ml.com"}},
	{"Wells Fargo", []string{"wells fargo", "wellsfargo"}, []string{"wellsfargo.com", "wf.com"}},
	{"Citibank", []string{"citibank", "citi bank"}, []string{"citi.com", "citibank.com", "citicards.com"}},
	{"Capital One", []string{"capital one", "capitalone"}, []string{"capitalone.com", "capitalonebank.com"}},
	{"American Express", []string{"american express", "amex"}, []string{"americanexpress.com", "aexp.com", "amex.com"}},
	{"USAA", []string{"usaa"}, []string{"usaa.com"}},
	{"IRS", []string{"internal revenue service", "irs"}, []string{"irs.gov"}},
	{"USPS", []string{"usps", "us postal service", "postal service"}, []string{"usps.com", "usps.gov"}},
	{"FedEx", []string{"fedex"}, []string{"fedex.com"}},
	{"UPS", []string{"ups delivery", "ups tracking", "united parcel service"}, []string{"ups.com"}},
	{"DHL", []string{"dhl"}, []string{"dhl.com", "dhl.de", "dhl.co.uk"}},
	{"Intuit", []string{"quickbooks", "intuit", "turbotax"}, []string{"intuit.com", "quickbooks.com", "turbotax.com"}},
	{"Norton", []string{"norton", "lifelock"}, []string{"norton.com", "nortonlifelock.com", "gendigital.com", "lifelock.com", "avast.com", "avg.com"}},
	{"McAfee", []string{"mcafee"}, []string{"mcafee.com", "mcafeesecure.com"}},
	{"Geek Squad", []string{"geek squad"}, []string{"geeksquad.com", "bestbuy.com"}},
	{"Steam", []string{"steam support", "steam team"}, []string{"steampowered.com", "steamcommunity.com", "valvesoftware.com"}},
}

// freeMail providers anyone can sign up for. A company does not write to its
// customers from one.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true,
	"yahoo.com": true, "ymail.com": true, "aol.com": true, "icloud.com": true, "me.com": true, "mail.com": true,
	"gmx.com": true, "gmx.net": true, "proton.me": true, "protonmail.com": true, "zoho.com": true,
	"yandex.com": true, "yandex.ru": true, "mail.ru": true, "qq.com": true, "163.com": true,
}

// riskyTLDs are cheap or abused top-level domains. They are only a small
// nudge: plenty of honest sites use them.
var riskyTLDs = map[string]bool{
	"zip": true, "mov": true, "top": true, "xyz": true, "click": true, "work": true, "gq": true, "tk": true,
	"ml": true, "cf": true, "ga": true, "icu": true, "rest": true, "buzz": true, "cyou": true, "monster": true,
	"sbs": true, "cfd": true, "bond": true,
}

// secondLevel lists public suffixes with two labels, so the "registrable
// domain" of mail.example.co.uk is example.co.uk.
var secondLevel = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "com.au": true, "net.au": true, "org.au": true,
	"co.nz": true, "co.jp": true, "co.in": true, "com.br": true, "com.mx": true, "co.za": true, "com.sg": true,
	"com.tr": true, "com.cn": true, "com.hk": true, "co.kr": true,
}

// apex returns the registrable domain: the part a company actually owns.
func apex(domain string) string {
	labels := strings.Split(strings.Trim(strings.ToLower(domain), "."), ".")
	if len(labels) <= 2 {
		return strings.Join(labels, ".")
	}
	if secondLevel[strings.Join(labels[len(labels)-2:], ".")] {
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// label is the name part of a registrable domain ("paypal" for paypal.com).
func label(apexDomain string) string {
	if i := strings.IndexByte(apexDomain, '.'); i > 0 {
		return apexDomain[:i]
	}
	return apexDomain
}

func tld(domain string) string {
	if i := strings.LastIndexByte(domain, '.'); i >= 0 {
		return domain[i+1:]
	}
	return domain
}

// inDomain reports whether d is domain or one of its subdomains.
func inDomain(d, domain string) bool {
	return d == domain || strings.HasSuffix(d, "."+domain)
}

func (b brand) owns(d string) bool {
	for _, dom := range b.domains {
		if inDomain(d, dom) {
			return true
		}
	}
	return false
}

// labels are the name parts of the brand's domains ("paypal", "amazon"…).
func (b brand) labels() map[string]bool {
	out := map[string]bool{}
	for _, dom := range b.domains {
		out[label(apex(dom))] = true
	}
	return out
}

// homoglyphs folds the substitutions lookalike domains rely on back to plain
// letters, so "paypa1" and "rnicrosoft" meet their originals.
var homoglyphs = strings.NewReplacer(
	"0", "o", "1", "l", "3", "e", "5", "s", "7", "t", "rn", "m", "vv", "w", "cl", "d",
	"-", "", "_", "",
)

func fold(s string) string { return homoglyphs.Replace(strings.ToLower(s)) }

// distance is the Levenshtein edit distance, giving up early past max.
func distance(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			low = min(low, cur[j])
		}
		if low > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
