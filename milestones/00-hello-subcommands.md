# Milestone 0 — Hello, subcommands

**Goal:** a `butler` program that knows its three subcommands, prints a usage
message when called incorrectly, and calls a stub function in its own
`crawler` package.

**Done when:**

```sh
$ go run .
usage: butler <crawl|index|ask> [args]
$ echo $?
1

$ go run . crawl https://example.com
crawling https://example.com (not implemented yet)

$ go run . index
index: not implemented yet

$ go run . ask "what is this site about?"
ask: what is this site about?

$ go run . dance
unknown command "dance"
usage: butler <crawl|index|ask> [args]
```

**Concepts:** modules, `package main`, imports, `os.Args`, slices, `switch`,
`fmt`, exit codes, exported names, importing your own package.

---

## Step 1: Create the module

A **module** is a Go project. It's defined by a `go.mod` file that records its
name (the *module path*) and its dependencies.

```sh
cd ~/Github/butler
go mod init butler
```

`butler` is fine as the module path for a local project. If you plan to publish
it, use `github.com/<your-username>/butler` instead. The module path becomes the
prefix of your import paths (`butler/internal/crawler`).

Open `go.mod` to see what was created. Go maintains it for you.

---

## Step 2: The smallest program

Create `main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("hello from butler")
}
```

Run it with `go run .` (the `.` means "the package in this folder").

What each part means:

- `package main`: every file starts by naming its package. `main` is special
  because it's the package that builds into a runnable program.
- `import "fmt"`: brings in the standard formatting/printing package. When you
  import several packages, use parentheses:
  ```go
  import (
  	"fmt"
  	"os"
  )
  ```
- `func main()`: where the program starts. It takes no arguments and returns
  nothing. Command-line arguments and exit codes go through the `os` package
  instead.
- Indentation is a **tab**. `gofmt` enforces this, so don't worry about it.

---

## Step 3: Read the arguments

`os.Args` is a **slice of strings** (`[]string`) holding the command line:

| You type | `os.Args` |
|---|---|
| `butler` | `["butler"]` |
| `butler crawl https://x.com` | `["butler", "crawl", "https://x.com"]` |

(With `go run .`, `os.Args[0]` is a temporary binary path, but the rest is the same.)

The syntax you need:

```go
len(os.Args)      // how many elements
os.Args[1]        // the second element. PANICS if it doesn't exist!
os.Args[2:]       // a sub-slice: everything from index 2 on (may be empty, never panics if 2 <= len)
```

**Try it:** print `len(os.Args)` and `os.Args` with `fmt.Println`, then run
it with different arguments.

⚠️ If you read `os.Args[1]` without checking the length first, `go run .` with
no arguments crashes with `index out of range [1] with length 1`. That's the
first thing your program needs to guard against.

---

## Step 4: Usage message and exit codes

When the program is used wrong, it should print to **stderr** (not stdout) and
exit with a non-zero code. That's the convention for every CLI.

```go
fmt.Fprintln(os.Stderr, "something went wrong")  // print to stderr
os.Exit(1)                                       // stop now with exit code 1
```

You'll print the usage line in more than one place, so put it in its own
function:

```go
func usage() {
	// your code: print the usage line to stderr
}
```

Lowercase `usage` means it's private to this package, which is fine here.

---

## Step 5: Dispatch with `switch`

Go's `switch` doesn't fall through to the next case, so there's no `break`
needed:

```go
switch x {
case "a":
	// ...
case "b", "c":     // several values in one case
	// ...
default:
	// nothing matched
}
```

Variables:

```go
cmd := os.Args[1]           // := declares and assigns; the type (string) is inferred
args := os.Args[2:]         // []string
```

Formatted printing:

```go
fmt.Printf("crawling %s\n", url)        // %s = string, \n = newline (Printf doesn't add one)
fmt.Printf("unknown command %q\n", cmd) // %q = string in quotes: "dance"
fmt.Println("a", "b")                   // spaces between values, newline at the end
```

**Your task:** in `main`:

1. If there's no subcommand, call `usage()` and exit 1.
2. `switch` on the subcommand:
   - `crawl`: needs exactly one more argument (the URL). If it's missing, print
     `usage: butler crawl <url>` to stderr and exit 1. Otherwise print the
     "not implemented yet" line.
   - `index`: print the placeholder.
   - `ask`: needs at least one more argument. Join them all into one question
     with `strings.Join(args, " ")` (import `"strings"`), so that
     `butler ask what is this` works even without quotes.
   - anything else: print `unknown command "..."` plus usage, exit 1.

---

## Step 6: Your first package

Now move the crawl placeholder into its own package, which is where the real
crawler will go in milestone 1.

Create `internal/crawler/crawler.go`:

```go
package crawler

// Crawl will fetch pages starting from start. For now it only prints.
func Crawl(start string) error {
	// your code: print the "crawling ... (not implemented yet)" line
	return nil
}
```

Notes:

- The package name (`crawler`) matches the folder name. That's the convention.
- `Crawl` is **capitalized**, so it's **exported** and other packages can call
  it. A lowercase `crawl` would only be visible inside `crawler`.
- `error` is a built-in type. Returning `nil` means "no error". It returns an
  error even though nothing can fail yet, because the real version will.
- A comment directly above an exported function, starting with its name, is its
  documentation. `go doc` shows it.

Import it in `main.go` using **module path + folder**:

```go
import (
	"fmt"
	"os"
	"strings"

	"butler/internal/crawler"
)
```

Then in the `crawl` case, call it and handle the error. This is Go's most
common pattern:

```go
if err := crawler.Crawl(args[0]); err != nil {
	fmt.Fprintln(os.Stderr, "crawl:", err)
	os.Exit(1)
}
```

(`if x := ...; condition` declares `x` only for the `if` block, which keeps
`err` from leaking into the rest of the function.)

---

## Step 7: Build and tidy

```sh
gofmt -w .        # format everything
go vet ./...      # check for common mistakes (./... = this folder and all below)
go build          # produces ./butler
./butler ask hello there
```

Add a `.gitignore` containing `butler` so the binary isn't committed.

---

## Errors you'll likely see

| Error | Meaning |
|---|---|
| `"strings" imported and not used` | Unused imports don't compile. Remove it or use it. |
| `declared and not used: args` | Same for variables. |
| `index out of range [1] with length 1` | You read `os.Args[1]` without checking `len` first. |
| `package butler/internal/crawler is not in std` | The import path doesn't match `go.mod`'s module path + folder. |
| `undefined: crawler.crawl` | Lowercase name, so it isn't exported. |
| `missing return` | The function declares `error` but some path doesn't `return`. |

---

## Check yourself

- Why does `go run .` with no arguments crash if you skip the length check?
- What's the difference between `fmt.Println` and `fmt.Fprintln(os.Stderr, ...)`?
- Why can `main.go` call `crawler.Crawl` but not `crawler.crawl`?
- What does `os.Args[2:]` give you when there are exactly 2 elements?

When everything in **Done when** works, send me your `main.go` and
`crawler.go` for a review, then we'll move on to milestone 1 (fetching a real
page).

<details>
<summary>Stuck? Shape of <code>main</code> (spoiler, not the full answer)</summary>

```go
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "crawl":
		// check len(args), then call crawler.Crawl
	case "index":
		// placeholder
	case "ask":
		// check len(args), join, print
	default:
		// unknown command
	}
}
```

</details>
