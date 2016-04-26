package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dickeyxxx/netrc"
	"github.com/dickeyxxx/speakeasy"
	"github.com/toqueteos/webbrowser"
)

func init() {
	Topics = append(Topics, TopicSet{{
		Name:        "auth",
		Description: "authentication (login/logout)",
		Commands: []*Command{
			{
				Command:     "login",
				Description: "login with your Heroku credentials.",
				Flags: []Flag{
					{Name: "sso", Description: "login for enterprise users under SSO"},
				},
				Run: login,
			},
			{
				Command:     "logout",
				Description: "clear your local Heroku credentials",
				Run:         logout,
			},
			{
				Command:     "whoami",
				Description: "display your Heroku login",
				Help: `Example:

  $ heroku auth:whoami
	email@example.com

	whoami will return nonzero status if not logged in:

  $ heroku auth:whoami
	not logged in
	$ echo $?
	100`,
				Run: whoami,
			},
			{
				Command:     "token",
				Description: "display your API token.",
				NeedsAuth:   true,
				Run: func(ctx *Context) {
					Println(ctx.APIToken)
				},
			},
			{
				Command:     "2fa:enable",
				Description: "enable 2fa on your account",
				NeedsAuth:   true,
				Run:         twoFactorRun,
			},
			{
				Command:     "2fa:gen-recovery-codes",
				Description: "generates and replaces recovery codes",
				NeedsAuth:   true,
				Run:         twoFactorGenerateRun,
			},
			{
				Command:     "2fa:disable",
				Description: "disable two-factor authentication for your account",
				NeedsAuth:   true,
				Run:         twoFactorDisableRun,
			},
		},
	},
		{
			Name:   "whoami",
			Hidden: true,
			Commands: []*Command{
				{
					Description: "display your Heroku login",
					Help: `Example:

  $ heroku auth:whoami
	email@example.com

	whoami will return nonzero status if not logged in:

  $ heroku auth:whoami
	not logged in
	$ echo $?
	100`,
					Run: whoami,
				},
			},
		},
		{
			Name:        "login",
			Description: "login with your Heroku credentials.",
			Commands: []*Command{
				{
					Description: "login with your Heroku credentials.",
					Flags: []Flag{
						{Name: "sso", Description: "login for enterprise users under SSO"},
					},
					Run: login,
				},
			},
		},
		{
			Name:        "logout",
			Hidden:      true,
			Description: "clear your local Heroku credentials",
			Commands: []*Command{
				{
					Description: "clear your local Heroku credentials",
					Run:         logout,
				},
			},
		},
		{
			Name:   "twofactor",
			Hidden: true,
			Commands: []*Command{
				&Command{
					NeedsAuth:   true,
					Description: "enable 2fa on your account",
					Run:         twoFactorRun,
				},
				&Command{
					Command:     "generate-recovery-codes",
					Description: "Generates and replaces recovery codes",
					NeedsAuth:   true,
					Run:         twoFactorGenerateRun,
				},
				&Command{
					Command:     "disable",
					Description: "Disable two-factor authentication for your account",
					NeedsAuth:   true,
					Run:         twoFactorDisableRun,
				},
			},
		},
		{
			Name:   "2fa",
			Hidden: true,
			Commands: []*Command{
				&Command{
					NeedsAuth:   true,
					Description: "enable 2fa on your account",
					Run:         twoFactorRun,
				},
				&Command{
					Command:     "generate-recovery-codes",
					Description: "Generates and replaces recovery codes",
					NeedsAuth:   true,
					Run:         twoFactorGenerateRun,
				},
				&Command{
					Command:     "disable",
					Description: "Disable two-factor authentication for your account",
					NeedsAuth:   true,
					Run:         twoFactorDisableRun,
				},
			},
		},
	}...,
	)
}

func whoami(ctx *Context) {
	if os.Getenv("HEROKU_API_KEY") != "" {
		Warn("HEROKU_API_KEY is set")
	}

	// don't use needsToken since this should fail if
	// not logged in. Should not show a login prompt.
	ctx.APIToken = apiToken()

	if ctx.APIToken == "" {
		Println("not logged in")
		Exit(100)
	}

	user := getUserFromToken(ctx.APIToken)
	if user == "" {
		Println("not logged in")
		Exit(100)
	}
	Println(user)
}

func login(ctx *Context) {
	if os.Getenv("HEROKU_API_KEY") != "" {
		Warn("HEROKU_API_KEY is set")
	}
	if ctx.Flags["sso"] == true {
		ssoLogin()
	} else {
		interactiveLogin()
	}
}

func ssoLogin() {
	url := os.Getenv("SSO_URL")
	if url == "" {
		org := os.Getenv("HEROKU_ORGANIZATION")
		for org == "" {
			org = getString("Enter your organization name: ")
		}
		url = "https://sso.heroku.com/saml/" + org + "/init?cli=true"
	}
	Err("Opening browser for login...")
	err := webbrowser.Open(url)
	if err != nil {
		Errln(" " + err.Error() + ".\nNavigate to " + cyan(url))
	} else {
		Errln(" done")
	}
	token := getPassword("Enter your access token (typing will be hidden): ")
	user := getUserFromToken(token)
	if user == "" {
		ExitIfError(errors.New("Access token invalid."))
	}
	saveOauthToken(user, token)
	Println("Logged in as " + cyan(user))
}

func getUserFromToken(token string) string {
	req := apiRequest(token)
	req.Method = "GET"
	req.Uri = req.Uri + "/account"
	res, err := req.Do()
	ExitIfError(err)
	if res.StatusCode != 200 {
		return ""
	}
	var doc map[string]interface{}
	res.Body.FromJsonTo(&doc)
	return doc["email"].(string)
}

func interactiveLogin() {
	if apiHost() == "api.heroku.com" {
		Println("Enter your Heroku credentials.")
	} else {
		Printf("Enter your Heroku credentials for %s.\n", apiHost())
	}
	email := getString("Email: ")
	password := getPassword("Password (typing will be hidden): ")

	token := v2login(email, password, "")
	// TODO: use createOauthToken (v3 API)
	// token, err := createOauthToken(email, password, "")
	saveOauthToken(email, token)
	Println("Logged in as " + cyan(email))
}

func saveOauthToken(email, token string) {
	netrc := getNetrc()
	netrc.RemoveMachine(apiHost())
	netrc.RemoveMachine(httpGitHost())
	netrc.AddMachine(apiHost(), email, token)
	netrc.AddMachine(httpGitHost(), email, token)
	ExitIfError(netrc.Save())
}

func getString(prompt string) string {
	var s string
	Err(prompt)
	if _, err := fmt.Scanln(&s); err != nil {
		if err.Error() == "unexpected newline" {
			return getString(prompt)
		}
		if err.Error() == "EOF" {
			Errln()
			Exit(1)
		}
		ExitIfError(err)
	}
	return s
}

func getPassword(prompt string) string {
	password, err := speakeasy.Ask(prompt)
	if err != nil {
		if err.Error() == "The handle is invalid." {
			Errln(`Login is currently incompatible with git bash/cygwin
In the meantime, login via cmd.exe
https://github.com/heroku/cli/issues/84`)
			Exit(1)
		} else {
			ExitIfError(err)
		}
	}
	return password
}

func v2login(email, password, secondFactor string) string {
	req := apiRequestBase("")
	req.Method = POST

	req.Uri = req.Uri + "/login"
	req.ContentType = "application/x-www-form-urlencoded"
	req.ShowDebug = false

	data := url.Values{}
	data.Set("username", email)
	data.Set("password", password)
	req.Body = data.Encode()

	if secondFactor != "" {
		req.AddHeader("Heroku-Two-Factor-Code", secondFactor)
	}
	res, err := req.Do()
	ExitIfError(err)
	switch res.StatusCode {
	case 200:
		var response struct {
			APIKey string `json:"api_key"`
		}
		ExitIfError(res.Body.FromJsonTo(&response))
		return response.APIKey
	case 401:
		var response struct {
			Error string `json:"error"`
		}
		ExitIfError(res.Body.FromJsonTo(&response))
		ExitWithMessage(response.Error)
		panic("unreachable")
	case 403:
		return v2login(email, password, getString("Two-factor code: "))
	case 404:
		ExitWithMessage("Authentication failed.\nEmail or password is not valid.\nCheck your credentials on https://dashboard.heroku.com")
		panic("unreachable")
	default:
		body, err := res.Body.ToString()
		WarnIfError(err)
		ExitWithMessage("Invalid response from API.\nHTTP %d\n%s\n\nAre you behind a proxy?\nhttps://devcenter.heroku.com/articles/using-the-cli#using-an-http-proxy", res.StatusCode, body)
		panic("unreachable")
	}
}

func createOauthToken(email, password, secondFactor string) (string, error) {
	req := apiRequest("")
	req.Method = POST
	req.Uri = req.Uri + "/oauth/authorizations"
	req.BasicAuthUsername = email
	req.BasicAuthPassword = password
	req.Body = map[string]interface{}{
		"scope":       []string{"global"},
		"description": "Heroku CLI login from " + time.Now().UTC().Format(time.RFC3339),
		"expires_in":  60 * 60 * 24 * 30, // 30 days
	}
	if secondFactor != "" {
		req.AddHeader("Heroku-Two-Factor-Code", secondFactor)
	}
	res, err := req.Do()
	ExitIfError(err)
	type Doc struct {
		ID          string
		Message     string
		AccessToken struct {
			Token string
		} `json:"access_token"`
	}
	var doc Doc
	res.Body.FromJsonTo(&doc)
	if doc.ID == "two_factor" {
		return createOauthToken(email, password, getString("Two-factor code: "))
	}
	if res.StatusCode != 201 {
		return "", errors.New(doc.Message)
	}
	return doc.AccessToken.Token, nil
}

func logout(ctx *Context) {
	if os.Getenv("HEROKU_API_KEY") != "" {
		Warn("HEROKU_API_KEY is set")
	}
	netrc := getNetrc()
	netrc.RemoveMachine(apiHost())
	netrc.RemoveMachine(httpGitHost())
	ExitIfError(netrc.Save())
	Println("Local credentials cleared.")
}

func getNetrc() *netrc.Netrc {
	n, err := netrc.Parse(netrcPath())
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			// File not found
			return &netrc.Netrc{Path: netrcPath()}
		}
		Errln("Error parsing netrc at " + netrcPath())
		Errln(err.Error())
		Exit(1)
	}
	return n
}

func auth() (password string) {
	token := apiToken()
	if token == "" {
		interactiveLogin()
		return auth()
	}
	return token
}

func apiToken() string {
	key := os.Getenv("HEROKU_API_KEY")
	if key != "" {
		return key
	}
	netrc := getNetrc()
	machine := netrc.Machine(apiHost())
	if machine != nil {
		return machine.Get("password")
	}
	return ""
}

func netrcPath() string {
	base := filepath.Join(HomeDir, ".netrc")
	if runtime.GOOS == WINDOWS {
		base = filepath.Join(HomeDir, "_netrc")
	}
	if exists, _ := fileExists(base + ".gpg"); exists {
		base = base + ".gpg"
	}
	return base
}

func netrcLogin() string {
	key := os.Getenv("HEROKU_API_KEY")
	if key != "" {
		return ""
	}
	netrc := getNetrc()
	machine := netrc.Machine(apiHost())
	if machine != nil {
		return machine.Get("login")
	}
	return ""
}

func twoFactorGenerateRun(ctx *Context) {
	req := apiRequest(ctx.APIToken)
	req.Method = POST
	req.Uri = req.Uri + "/account/recovery-codes"
	req.AddHeader("Heroku-Password", getPassword("Password (typing will be hidden): "))
	req.AddHeader("Heroku-Two-Factor-Code", getString("Two-factor code: "))
	res, err := req.Do()
	ExitIfError(err)
	var codes []interface{}
	res.Body.FromJsonTo(&codes)
	Println("Recovery codes:")
	for _, code := range codes {
		Println(code)
	}
}

func twoFactorDisableRun(ctx *Context) {
	req := apiRequest(ctx.APIToken)
	req.Method = "PATCH"
	req.Uri = req.Uri + "/account/"
	req.Body = map[string]interface{}{
		"two_factor_authentication": "false",
		"password":                  getPassword("Password (typing will be hidden):"),
	}
	res, err := req.Do()
	ExitIfError(err)
	if res.StatusCode != 200 {
		var doc map[string]string
		res.Body.FromJsonTo(&doc)
		Error(doc["message"])
		return
	}
	Println("disabled two-factor authentication")
}

func twoFactorRun(ctx *Context) {
	req := apiRequest(ctx.APIToken)
	req.Method = "GET"
	req.Uri = req.Uri + "/account"
	res, err := req.Do()
	ExitIfError(err)
	var doc map[string]bool
	res.Body.FromJsonTo(&doc)
	if doc["two_factor_authentication"] {
		Println("Two-factor authentication is enabled")
	} else {
		Println("Two-factor authentication is not enabled")
	}
}