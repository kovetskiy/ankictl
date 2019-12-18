# ankictl

Command line interface for ankiweb.net, inspired by
[runki](https://github.com/seletskiy/runki) but does not translate anything.

## Usage

```
ankictl -A <deck>
```

There are 2 allowed formats for stdin: text (default) and json.

```
ankictl -A <deck> -i '["front","back"]'
```

If input from optional argument, input value in json format.
Input is enclosed in ''.

## Format: text

```
<front>\t<back>
```

Where `\t` is real tab separating char.

## Format: json

Every line should be formatted as array of 2 strings.

```json
["front", "back"]
["front2", "back2"]
```


## Configuration

Config should be located in ~/.config/anki/anki.conf, format:

```toml
email = "yellow@carcosa"
password = "fl4t-c1rcl3"
```

## Installation

```
go get github.com/kovetskiy/ankictl
```

Arch Linux users can install package from aur:

```
yaourt -Sy ankictl-git
```
