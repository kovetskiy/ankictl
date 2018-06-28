package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kovetskiy/biscuitjar"
	"github.com/reconquest/karma-go"
)

const (
	DefaultDeckModelID = "1510000287133"

	UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/56.0.2924.87 Safari/537.36"

	URLLogin       = "https://ankiweb.net/account/login"
	URLEdit        = "https://ankiuser.net/edit/"
	URLEditSave    = "https://ankiuser.net/edit/save"
	URLCheckCookie = "https://ankiweb.net/account/checkCookie"
	URLSearch      = "https://ankiweb.net/search/"
)

var (
	reTokenEdit  = regexp.MustCompile("editor.csrf_token2 = '([^']+)';")
	reTokenLogin = regexp.MustCompile(`name="csrf_token" value="([^"]+)`)
	reItem       = regexp.MustCompile(`<td> ([^/]+)`)
)

type Anki struct {
	cookiesJar    *biscuitjar.Jar
	cookiesExists bool
	client        *http.Client

	adding bool
	token  string // csrf token
}

func NewAnki(cookies string) (*Anki, error) {
	anki := &Anki{}

	jar, err := biscuitjar.New(nil)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to create cookies jar",
		)
	}

	anki.cookiesJar = jar

	file, err := os.OpenFile(cookies, os.O_RDWR, 0600)
	if err != nil && !os.IsNotExist(err) {
		return nil, karma.Format(
			err,
			"unable to open cookies file",
		)
	}

	if !os.IsNotExist(err) {
		err = jar.Read(file)
		if err != nil {
			return nil, karma.Format(
				err,
				"unable to parse cookies file",
			)
		}

		anki.cookiesExists = true
	}

	anki.client = &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if anki.adding {
				return nil
			}

			return http.ErrUseLastResponse
		},
	}

	return anki, nil
}

func (anki *Anki) Login(email, password string) error {
	log.Debugf("requesting %s", URLLogin)

	request, err := http.NewRequest("GET", URLLogin, nil)
	if err != nil {
		return karma.Format(
			err,
			"unable to create request",
		)
	}

	addHeaderUserAgent(request)

	response, err := anki.client.Get(URLLogin)
	if err != nil {
		return karma.Describe("url", URLLogin).Format(
			err,
			"unable to request login page",
		)
	}

	if response.StatusCode == http.StatusFound {
		log.Debugf("status is %s, already authorized", response.Status)
		return nil
	}

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return karma.Format(
			err,
			"unable to read response",
		)
	}

	log.Tracef("%s response: %s", URLLogin, string(contents))

	matches := reTokenLogin.FindStringSubmatch(string(contents))
	if len(matches) != 2 {
		return errors.New("unable to find token by regexp")
	}

	token := matches[1]

	payload := url.Values{}
	payload.Set("username", email)
	payload.Set("password", password)
	payload.Set("csrf_token", token)
	payload.Set("submitted", "1") // yep, submitted, of course

	log.Tracef("form: %s", payload.Encode())

	log.Debugf("posting form %s", URLLogin)

	request, err = http.NewRequest(
		"POST",
		URLLogin,
		bytes.NewBufferString(payload.Encode()),
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to create request",
		)
	}

	addHeaderUserAgent(request)
	addHeaderForm(request)
	addHeaderReferer(request, URLLogin)

	response, err = anki.client.Do(request)
	if err != nil {
		return karma.Describe("url", URLLogin).Format(
			err,
			"unable to request login page",
		)
	}

	log.Debugf("%s status %s", URLLogin, response.Status)

	if response.StatusCode != http.StatusFound {
		return errors.New("bad email/password")
	}

	return nil
}

func (anki *Anki) Add(deck, front, back string) error {
	if anki.token == "" {
		err := anki.prepareAdd()
		if err != nil {
			return karma.Format(
				err,
				"unable to prepare for adding",
			)
		}
	}

	context := karma.Describe("url", URLEditSave)

	data := fmt.Sprintf(`[[%q,%q],""]`, front, back)

	payload := url.Values{}
	payload.Set("csrf_token", anki.token)
	payload.Set("mid", DefaultDeckModelID)
	payload.Set("deck", deck)
	payload.Set("data", data)

	log.Tracef("form: %s", payload.Encode())

	log.Debugf("posting form %s", URLEditSave)

	request, err := http.NewRequest(
		"POST",
		URLEditSave,
		bytes.NewBufferString(payload.Encode()),
	)
	if err != nil {
		return context.Format(
			err,
			"unable to create request",
		)
	}

	addCookies(request, anki.cookiesJar)
	addHeaderUserAgent(request)
	addHeaderForm(request)
	addHeaderReferer(request, URLEdit)
	addHeaderXML(request)

	response, err := anki.client.Do(request)
	if err != nil {
		return context.Format(
			err,
			"unable to request adding page",
		)
	}

	if response.StatusCode == http.StatusTooManyRequests {
		log.Debugf("got too many requests error, sleeping 3 seconds")
		time.Sleep(time.Second * 3)
		return anki.Add(deck, front, back)
	}

	log.Debugf("%s status: %s", URLEditSave, response.Status)

	log.Tracef("%s response: %s", URLEditSave, readall(response.Body))

	if response.StatusCode != http.StatusOK {
		return context.Format(nil,
			"server returned %s status, but 200 OK expected",
			response.Status,
		)
	}

	return nil
}

func checkStatus(response *http.Response) error {
	if response.StatusCode == http.StatusFound {
		return fmt.Errorf(
			"%s redirected to %s",
			response.Request.URL.String(),
			response.Header.Get("Location"),
		)
	}

	return nil
}

func (anki *Anki) prepareAdd() error {
	anki.adding = true
	defer func() {
		anki.adding = false
	}()

	context := karma.Describe("url", URLEdit)

	request, err := http.NewRequest("GET", URLEdit, nil)
	if err != nil {
		return context.Format(
			err,
			"unable to create request",
		)
	}

	addCookies(request, anki.cookiesJar)
	addHeaderUserAgent(request)

	response, err := anki.client.Get(URLEdit)
	if err != nil {
		return context.Format(
			err,
			"unable to request adding page",
		)
	}

	err = checkStatus(response)
	if err != nil {
		return context.Reason(err)
	}

	if response.StatusCode != http.StatusOK {
		return context.Format(response.Status, "expected status is 200 OK")
	}

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return context.Format(
			err,
			"unable to read response body",
		)
	}

	matches := reTokenEdit.FindStringSubmatch(string(contents))
	if len(matches) != 2 {
		return errors.New("unable to find csrf_token")
	}

	anki.token = matches[1]

	return nil
}

func (anki *Anki) Search(query string) (bool, error) {
	context := karma.Describe("url", URLSearch)

	payload := url.Values{
		"keyword":   {query},
		"submitted": {"1"},
	}

	log.Tracef("form: %s", payload.Encode())

	log.Debugf("posting form %s", URLSearch)

	request, err := http.NewRequest(
		"POST",
		URLSearch,
		bytes.NewBufferString(payload.Encode()),
	)
	if err != nil {
		return false, context.Format(
			err,
			"unable to create request",
		)
	}

	addCookies(request, anki.cookiesJar)
	addHeaderUserAgent(request)
	addHeaderForm(request)
	addHeaderReferer(request, URLSearch)
	addHeaderXML(request)

	response, err := anki.client.Do(request)
	if err != nil {
		return false, context.Format(
			err,
			"unable to request search page",
		)
	}

	log.Debugf("%s status: %s", URLSearch, response.Status)

	if response.StatusCode == http.StatusTooManyRequests {
		log.Debugf("got too many requests error, sleeping 3 seconds")
		time.Sleep(time.Second * 3)
		return anki.Search(query)
	}

	err = checkStatus(response)
	if err != nil {
		return false, context.Reason(err)
	}

	if response.StatusCode != http.StatusOK {
		return false, context.Format(nil,
			"server returned %s status, but 200 OK expected",
			response.Status,
		)
	}

	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return false, context.Format(
			err,
			"unable to read response body",
		)
	}

	log.Tracef("%s response: %s", URLSearch, contents)

	items := reItem.FindAllSubmatch(contents, -1)
	for _, m := range items {
		if strings.TrimSpace(string(m[1])) == query {
			return true, nil
		}
	}

	return false, nil
}

func (anki *Anki) SaveCookies(path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return karma.Describe("dir", filepath.Dir(path)).Format(
			err,
			"unable to mkdir",
		)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return karma.Format(
			err,
			"unable to create cookies file",
		)
	}

	err = anki.cookiesJar.Write(file)
	if err != nil {
		return karma.Format(
			err,
			"unable to write cookies",
		)
	}

	err = file.Close()
	if err != nil {
		return karma.Format(
			err,
			"unable to close cookies file",
		)
	}

	return nil
}

func addCookies(request *http.Request, jar *biscuitjar.Jar) {
	for url, cookies := range jar.CookiesAll() {
		if url.Host == request.URL.Host {
			for _, cookie := range cookies {
				if _, err := request.Cookie(cookie.Name); err != nil {
					request.AddCookie(cookie)
				}
			}
		}
	}
}

func readall(reader io.Reader) string {
	contents, _ := ioutil.ReadAll(reader)
	return string(contents)
}

func addHeaderReferer(request *http.Request, referer string) {
	request.Header.Set("Referer", referer)
}

func addHeaderUserAgent(request *http.Request) {
	request.Header.Set("User-Agent", UserAgent)
}

func addHeaderForm(request *http.Request) {
	request.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded; charset=UTF-8",
	)
}

func addHeaderXML(request *http.Request) {
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
}
