package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/docopt/docopt-go"
	"github.com/kovetskiy/ko"
	"github.com/kovetskiy/lorg"
	"github.com/reconquest/colorgful"
	"github.com/reconquest/karma-go"
)

var (
	version = "[manual build]"
	usage   = "ankictl " + version + `

ankiweb.net command line interface.

Usage:
  ankictl [options] -A <deck>
  ankictl -h | --help
  ankictl --version

Options:
  -A --add <deck>       Add cards into deck.
  -f --format <format>  Format of input. Can be text or json. [default: text]
  -c --config <path>    Use specified configuration file.
                         [default: $HOME/.config/anki/anki.conf]
  -k --cookies <path>   Use specified path for storing cookies.
                         [default: $HOME/.cache/anki/anki.cookies]
  --stop-streak <n>     Stop adding words when found streak
                         of <n> words already added.
                         [default: 10]
  --debug               Show debug messages.
  --trace               Show trace and debug messages.
  -h --help             Show this screen.
  --version             Show version.
`
)

var (
	log = lorg.NewLog()
)

type Config struct {
	Email    string `toml:"email" required:"true"`
	Password string `toml:"password" required:"true"`
}

func main() {
	args, err := docopt.Parse(os.ExpandEnv(usage), nil, true, version, false, true)
	if err != nil {
		panic(err)
	}

	log.SetIndentLines(true)

	log.SetFormat(
		colorgful.MustApplyDefaultTheme(
			"${time} ${level:[%s]:right:short} ${prefix}%s",
			colorgful.Dark,
		),
	)

	if args["--debug"].(bool) {
		log.SetLevel(lorg.LevelDebug)
	}

	if args["--trace"].(bool) {
		log.SetLevel(lorg.LevelTrace)
	}

	var config Config
	err = ko.Load(args["--config"].(string), &config)
	if err != nil {
		log.Fatal(karma.Format(err, "unable to load config"))
	}

	anki, err := NewAnki(args["--cookies"].(string))
	if err != nil {
		log.Fatal(karma.Format(err, "unable to initialize client"))
	}

	err = anki.Login(config.Email, config.Password)
	if err != nil {
		log.Fatal(karma.Format(err, "unable to login to ankiweb"))
	}

	var (
		maxStreak, _ = strconv.Atoi(args["--stop-streak"].(string))
		nowStreak    = 0
		newWords     = 0
	)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		target := scanner.Text()

		addNewWords(target, &nowStreak, &newWords, anki, args)

		if nowStreak == maxStreak {
			log.Debugf("got streak of existing words, stopping")
			break
		}
	}


	fmt.Printf("%d new words\n", newWords)

	err = anki.SaveCookies(args["--cookies"].(string))
	if err != nil {
		log.Fatal(
			karma.Format(
				err, "unable to save cookies to %s", args["--cookies"].(string),
			),
		)
	}
}

func addNewWords(target string, nowStreak *int, newWords *int, anki *Anki, args map[string]interface{}) (error) {
	if target == "" {
		return nil
	}

	front, back, err := getFrontBack(target, args["--format"].(string))
	if err != nil {
		log.Fatal(err)
	}

	found, err := anki.Search(front)
	if err != nil {
		log.Fatal(karma.Format(err, "unable to search %q", front))
	}

	if found {
		log.Debugf("%q is already exists", front)
		*nowStreak++
		return nil
	}

	*nowStreak = 0

	log.Debugf("adding %q: %q", front, back)

	err = anki.Add(args["--add"].(string), front, back)
	if err != nil {
		log.Fatal(karma.Format(err, "unable to add %q: %q", front, back))
	}

	*newWords++

	return nil
}

func getFrontBack(target string, format string) (string, string, error) {
	if format == "json" {
		item := [2]string{}
		err := json.Unmarshal([]byte(target), &item)
		if err != nil {
			return "", "", err
		}

		return item[0], item[1], nil
	}

	item := strings.SplitN(target, "\t", 2)
	if len(item) > 1 {
		return item[0], item[1], nil
	}

	return item[0], "", nil
}
