// Copyright (c) 2015 Spinpunch, Inc. All Rights Reserved.
// See License.txt for license information.

package web

import (
	l4g "code.google.com/p/log4go"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/mattermost/platform/api"
	"github.com/mattermost/platform/model"
	"github.com/mattermost/platform/utils"
	"github.com/mssola/user_agent"
	"gopkg.in/fsnotify.v1"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

var Templates *template.Template

type HtmlTemplatePage api.Page

func NewHtmlTemplatePage(templateName string, title string) *HtmlTemplatePage {

	if len(title) > 0 {
		title = utils.Cfg.ServiceSettings.SiteName + " - " + title
	}

	props := make(map[string]string)
	props["AnalyticsUrl"] = utils.Cfg.ServiceSettings.AnalyticsUrl
	return &HtmlTemplatePage{TemplateName: templateName, Title: title, SiteName: utils.Cfg.ServiceSettings.SiteName, Props: props}
}

func (me *HtmlTemplatePage) Render(c *api.Context, w http.ResponseWriter) {
	if err := Templates.ExecuteTemplate(w, me.TemplateName, me); err != nil {
		c.SetUnknownError(me.TemplateName, err.Error())
	}
}

func InitWeb() {
	l4g.Debug("Initializing web routes")

	staticDir := utils.FindDir("web/static")
	l4g.Debug("Using static directory at %v", staticDir)
	api.Srv.Router.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
		http.FileServer(http.Dir(staticDir))))

	api.Srv.Router.Handle("/", api.AppHandler(root)).Methods("GET")
	api.Srv.Router.Handle("/login", api.AppHandler(login)).Methods("GET")
	api.Srv.Router.Handle("/signup_team_confirm/", api.AppHandler(signupTeamConfirm)).Methods("GET")
	api.Srv.Router.Handle("/signup_team_complete/", api.AppHandler(signupTeamComplete)).Methods("GET")
	api.Srv.Router.Handle("/signup_user_complete/", api.AppHandler(signupUserComplete)).Methods("GET")

	api.Srv.Router.Handle("/logout", api.AppHandler(logout)).Methods("GET")

	api.Srv.Router.Handle("/verify", api.AppHandler(verifyEmail)).Methods("GET")
	api.Srv.Router.Handle("/find_team", api.AppHandler(findTeam)).Methods("GET")
	api.Srv.Router.Handle("/reset_password", api.AppHandler(resetPassword)).Methods("GET")

	csr := api.Srv.Router.PathPrefix("/channels").Subrouter()
	csr.Handle("/{name:[A-Za-z0-9-]+(__)?[A-Za-z0-9-]+}", api.UserRequired(getChannel)).Methods("GET")

	watchAndParseTemplates()
}

func watchAndParseTemplates() {

	templatesDir := utils.FindDir("web/templates")
	l4g.Debug("Parsing templates at %v", templatesDir)
	var err error
	if Templates, err = template.ParseGlob(templatesDir + "*.html"); err != nil {
		l4g.Error("Failed to parse templates %v", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		l4g.Error("Failed to create directory watcher %v", err)
	}

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Write == fsnotify.Write {
					l4g.Info("Re-parsing templates because of modified file %v", event.Name)
					if Templates, err = template.ParseGlob(templatesDir + "*.html"); err != nil {
						l4g.Error("Failed to parse templates %v", err)
					}
				}
			case err := <-watcher.Errors:
				l4g.Error("Failed in directory watcher %v", err)
			}
		}
	}()

	err = watcher.Add(templatesDir)
	if err != nil {
		l4g.Error("Failed to add directory to watcher %v", err)
	}
}

var browsersNotSupported string = "MSIE/8;MSIE/9;Internet Explorer/8;Internet Explorer/9"

func CheckBrowserCompatability(c *api.Context, r *http.Request) bool {
	ua := user_agent.New(r.UserAgent())
	bname, bversion := ua.Browser()

	browsers := strings.Split(browsersNotSupported, ";")
	for _, browser := range browsers {
		version := strings.Split(browser, "/")

		if strings.HasPrefix(bname, version[0]) && strings.HasPrefix(bversion, version[1]) {
			c.Err = model.NewAppError("CheckBrowserCompatability", "Your current browser is not supported, please upgrade to one of the following browsers: Google Chrome 21 or higher, Internet Explorer 10 or higher, FireFox 14 or higher", "")
			return false
		}
	}

	return true

}

func root(c *api.Context, w http.ResponseWriter, r *http.Request) {

	if !CheckBrowserCompatability(c, r) {
		return
	}

	if len(c.Session.UserId) == 0 {
		if api.IsTestDomain(r) || strings.Index(r.Host, "www") == 0 || strings.Index(r.Host, "beta") == 0 || strings.Index(r.Host, "ci") == 0 {
			page := NewHtmlTemplatePage("signup_team", "Signup")
			page.Render(c, w)
		} else {
			login(c, w, r)
		}
	} else {
		page := NewHtmlTemplatePage("home", "Home")
		page.Render(c, w)
	}
}

func login(c *api.Context, w http.ResponseWriter, r *http.Request) {
	if !CheckBrowserCompatability(c, r) {
		return
	}

	teamName := "Beta"
	teamDomain := ""
	siteDomain := "." + utils.Cfg.ServiceSettings.Domain

	if utils.Cfg.ServiceSettings.Mode == utils.MODE_DEV {
		teamDomain = "developer"
	} else if utils.Cfg.ServiceSettings.Mode == utils.MODE_BETA {
		teamDomain = "beta"
	} else {
		teamDomain, siteDomain = model.GetSubDomain(c.TeamUrl)
		siteDomain = "." + siteDomain + ".com"

		if tResult := <-api.Srv.Store.Team().GetByDomain(teamDomain); tResult.Err != nil {
			l4g.Error("Couldn't find team teamDomain=%v, siteDomain=%v, teamUrl=%v, err=%v", teamDomain, siteDomain, c.TeamUrl, tResult.Err.Message)
		} else {
			teamName = tResult.Data.(*model.Team).Name
		}
	}

	page := NewHtmlTemplatePage("login", "Login")
	page.Props["TeamName"] = teamName
	page.Props["TeamDomain"] = teamDomain
	page.Props["SiteDomain"] = siteDomain
	page.Render(c, w)
}

func signupTeamConfirm(c *api.Context, w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")

	page := NewHtmlTemplatePage("signup_team_confirm", "Signup Email Sent")
	page.Props["Email"] = email
	page.Render(c, w)
}

func signupTeamComplete(c *api.Context, w http.ResponseWriter, r *http.Request) {
	data := r.FormValue("d")
	hash := r.FormValue("h")

	if !model.ComparePassword(hash, fmt.Sprintf("%v:%v", data, utils.Cfg.ServiceSettings.InviteSalt)) {
		c.Err = model.NewAppError("signupTeamComplete", "The signup link does not appear to be valid", "")
		return
	}

	props := model.MapFromJson(strings.NewReader(data))

	t, err := strconv.ParseInt(props["time"], 10, 64)
	if err != nil || model.GetMillis()-t > 1000*60*60 { // one hour
		c.Err = model.NewAppError("signupTeamComplete", "The signup link has expired", "")
		return
	}

	page := NewHtmlTemplatePage("signup_team_complete", "Complete Team Sign Up")
	page.Props["Email"] = props["email"]
	page.Props["Name"] = props["name"]
	page.Props["Data"] = data
	page.Props["Hash"] = hash
	page.Render(c, w)
}

func signupUserComplete(c *api.Context, w http.ResponseWriter, r *http.Request) {

	id := r.FormValue("id")
	data := r.FormValue("d")
	hash := r.FormValue("h")
	var props map[string]string

	if len(id) > 0 {
		props = make(map[string]string)

		if result := <-api.Srv.Store.Team().Get(id); result.Err != nil {
			c.Err = result.Err
			return
		} else {
			team := result.Data.(*model.Team)
			if !(team.Type == model.TEAM_OPEN || (team.Type == model.TEAM_INVITE && len(team.AllowedDomains) > 0)) {
				c.Err = model.NewAppError("signupUserComplete", "The team type doesn't allow open invites", "id="+id)
				return
			}

			props["email"] = ""
			props["name"] = team.Name
			props["domain"] = team.Domain
			props["id"] = team.Id
			data = model.MapToJson(props)
			hash = ""
		}
	} else {

		if !model.ComparePassword(hash, fmt.Sprintf("%v:%v", data, utils.Cfg.ServiceSettings.InviteSalt)) {
			c.Err = model.NewAppError("signupTeamComplete", "The signup link does not appear to be valid", "")
			return
		}

		props = model.MapFromJson(strings.NewReader(data))

		t, err := strconv.ParseInt(props["time"], 10, 64)
		if err != nil || model.GetMillis()-t > 1000*60*60*48 { // 48 hour
			c.Err = model.NewAppError("signupTeamComplete", "The signup link has expired", "")
			return
		}
	}

	page := NewHtmlTemplatePage("signup_user_complete", "Complete User Sign Up")
	page.Props["Email"] = props["email"]
	page.Props["TeamName"] = props["name"]
	page.Props["TeamDomain"] = props["domain"]
	page.Props["TeamId"] = props["id"]
	page.Props["Data"] = data
	page.Props["Hash"] = hash
	page.Render(c, w)
}

func logout(c *api.Context, w http.ResponseWriter, r *http.Request) {
	api.Logout(c, w, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func getChannel(c *api.Context, w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	name := params["name"]

	var channelId string
	if result := <-api.Srv.Store.Channel().CheckPermissionsToByName(c.Session.TeamId, name, c.Session.UserId); result.Err != nil {
		c.Err = result.Err
		return
	} else {
		channelId = result.Data.(string)
	}

	if len(channelId) == 0 {
		if strings.Index(name, "__") > 0 {
			// It's a direct message channel that doesn't exist yet so let's create it
			ids := strings.Split(name, "__")
			otherUserId := ""
			if ids[0] == c.Session.UserId {
				otherUserId = ids[1]
			} else {
				otherUserId = ids[0]
			}

			if sc, err := api.CreateDirectChannel(c, otherUserId); err != nil {
				api.Handle404(w, r)
				return
			} else {
				channelId = sc.Id
			}
		} else {

			// lets make sure the user is valid
			if result := <-api.Srv.Store.User().Get(c.Session.UserId); result.Err != nil {
				c.Err = result.Err
				c.RemoveSessionCookie(w)
				l4g.Error("Error in getting users profile for id=%v forcing logout", c.Session.UserId)
				return
			}

			api.Handle404(w, r)
			return
		}
	}

	var team *model.Team

	if tResult := <-api.Srv.Store.Team().Get(c.Session.TeamId); tResult.Err != nil {
		c.Err = tResult.Err
		return
	} else {
		team = tResult.Data.(*model.Team)
	}

	page := NewHtmlTemplatePage("channel", "")
	page.Title = name + " - " + team.Name + " " + page.SiteName
	page.Props["TeamName"] = team.Name
	page.Props["TeamType"] = team.Type
	page.Props["TeamId"] = team.Id
	page.Props["ChannelName"] = name
	page.Props["ChannelId"] = channelId
	page.Props["UserId"] = c.Session.UserId
	page.Render(c, w)
}

func verifyEmail(c *api.Context, w http.ResponseWriter, r *http.Request) {
	resend := r.URL.Query().Get("resend")
	domain := r.URL.Query().Get("domain")
	email := r.URL.Query().Get("email")
	hashedId := r.URL.Query().Get("hid")
	userId := r.URL.Query().Get("uid")

	if resend == "true" {

		teamId := ""
		if result := <-api.Srv.Store.Team().GetByDomain(domain); result.Err != nil {
			c.Err = result.Err
			return
		} else {
			teamId = result.Data.(*model.Team).Id
		}

		if result := <-api.Srv.Store.User().GetByEmail(teamId, email); result.Err != nil {
			c.Err = result.Err
			return
		} else {
			user := result.Data.(*model.User)
			api.FireAndForgetVerifyEmail(user.Id, strings.Split(user.FullName, " ")[0], user.Email, domain, c.TeamUrl)
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	var isVerified string
	if len(userId) != 26 {
		isVerified = "false"
	} else if len(hashedId) == 0 {
		isVerified = "false"
	} else if model.ComparePassword(hashedId, userId) {
		isVerified = "true"
		if c.Err = (<-api.Srv.Store.User().VerifyEmail(userId)).Err; c.Err != nil {
			return
		} else {
			c.LogAudit("")
		}
	} else {
		isVerified = "false"
	}

	page := NewHtmlTemplatePage("verify", "Email Verified")
	page.Props["IsVerified"] = isVerified
	page.Render(c, w)
}

func findTeam(c *api.Context, w http.ResponseWriter, r *http.Request) {
	page := NewHtmlTemplatePage("find_team", "Find Team")
	page.Render(c, w)
}

func resetPassword(c *api.Context, w http.ResponseWriter, r *http.Request) {
	isResetLink := true
	hash := r.URL.Query().Get("h")
	data := r.URL.Query().Get("d")

	if len(hash) == 0 || len(data) == 0 {
		isResetLink = false
	} else {
		if !model.ComparePassword(hash, fmt.Sprintf("%v:%v", data, utils.Cfg.ServiceSettings.ResetSalt)) {
			c.Err = model.NewAppError("resetPassword", "The reset link does not appear to be valid", "")
			return
		}

		props := model.MapFromJson(strings.NewReader(data))

		t, err := strconv.ParseInt(props["time"], 10, 64)
		if err != nil || model.GetMillis()-t > 1000*60*60 { // one hour
			c.Err = model.NewAppError("resetPassword", "The signup link has expired", "")
			return
		}
	}

	teamName := "Developer/Beta"
	domain := ""
	if utils.Cfg.ServiceSettings.Mode != utils.MODE_DEV {
		domain, _ = model.GetSubDomain(c.TeamUrl)

		var team *model.Team
		if tResult := <-api.Srv.Store.Team().GetByDomain(domain); tResult.Err != nil {
			c.Err = tResult.Err
			return
		} else {
			team = tResult.Data.(*model.Team)
		}

		if team != nil {
			teamName = team.Name
		}
	}

	page := NewHtmlTemplatePage("password_reset", "")
	page.Title = "Reset Password - " + page.SiteName
	page.Props["TeamName"] = teamName
	page.Props["Hash"] = hash
	page.Props["Data"] = data
	page.Props["Domain"] = domain
	page.Props["IsReset"] = strconv.FormatBool(isResetLink)
	page.Render(c, w)
}
