package cli

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/magarcia/cuentica-cli/internal/catalog"
	"github.com/spf13/pflag"
)

type completionFlag struct {
	name, short, description string
	takesValue               bool
}

type completionModel struct {
	commands       []string
	actions        map[string][]string
	flags          []completionFlag
	flagNames      []string
	valueFlags     []string
	operationFlags map[string][]string
}

func (a *App) completion(words []string, help bool) error {
	shells := map[string]bool{"bash": true, "zsh": true, "fish": true, "powershell": true}
	if len(words) > 2 || len(words) == 2 && !shells[words[1]] {
		return usage("Use cuentica completion bash, zsh, fish, or powershell.")
	}
	if help || len(words) == 1 {
		_, err := fmt.Fprintln(a.Out, `Examples:
  source <(cuentica completion bash)
  cuentica completion zsh > _cuentica
  cuentica completion fish > ~/.config/fish/completions/cuentica.fish
  cuentica completion powershell > cuentica.ps1

Usage:
  cuentica completion <bash|zsh|fish|powershell>

Bash: source the generated script in your shell startup file.
Zsh: place _cuentica on your fpath and run compinit.
Fish: save the script in your fish completions directory.
PowerShell: dot-source the generated script from your profile.`)
		return err
	}
	model := a.completionModel()
	var script string
	switch words[1] {
	case "bash":
		script = bashCompletion(model)
	case "zsh":
		script = zshCompletion(model)
	case "fish":
		script = fishCompletion(model)
	case "powershell":
		script = powershellCompletion(model)
	}
	_, err := io.WriteString(a.Out, script)
	return err
}

func (a *App) completionModel() completionModel {
	model := completionModel{
		commands:       append(apiResources(), "api", "auth", "completion", "environment", "exit-codes", "help"),
		actions:        make(map[string][]string),
		operationFlags: make(map[string][]string),
	}
	sort.Strings(model.commands)
	model.actions["api"] = []string{"list"}
	model.actions["auth"] = []string{"login", "status", "logout"}
	model.actions["completion"] = []string{"bash", "zsh", "fish", "powershell"}
	model.actions["help"] = append(apiResources(), "environment", "exit-codes")
	for _, op := range catalog.Operations() {
		model.actions[op.Resource] = append(model.actions[op.Resource], op.Action)
		fs, _ := a.flags(&op)
		model.operationFlags[op.Resource+" "+op.Action] = flagNames(fs)
	}
	fs, _ := a.flags(nil)
	model.flagNames = flagNames(fs)
	fs.VisitAll(func(flag *pflag.Flag) {
		takesValue := flag.NoOptDefVal == ""
		model.flags = append(model.flags, completionFlag{flag.Name, flag.Shorthand, flag.Usage, takesValue})
		if takesValue {
			model.valueFlags = append(model.valueFlags, "--"+flag.Name)
			if flag.Shorthand != "" {
				model.valueFlags = append(model.valueFlags, "-"+flag.Shorthand)
			}
		}
	})
	return model
}

func flagNames(fs *pflag.FlagSet) []string {
	var names []string
	fs.VisitAll(func(flag *pflag.Flag) {
		names = append(names, "--"+flag.Name)
		if flag.Shorthand != "" {
			names = append(names, "-"+flag.Shorthand)
		}
	})
	return names
}

func bashCompletion(model completionModel) string {
	var script strings.Builder
	script.WriteString(`# Bash completion for cuentica.
_cuentica_complete() {
  local cur prev resource="" action="" word choices="" skip=0 i candidate
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  COMPREPLY=()
  case "$prev" in
    --output|-o) COMPREPLY=( $(compgen -W 'text json llm' -- "$cur") ); return ;;
    --save|--upload|--key-file)
      while IFS= read -r candidate; do COMPREPLY+=("$candidate"); done < <(compgen -f -- "$cur")
      return ;;
`)
	fmt.Fprintf(&script, "    %s) return ;;\n", strings.Join(model.valueFlags, "|"))
	script.WriteString(`  esac
  for (( i=1; i<COMP_CWORD; i++ )); do
    word="${COMP_WORDS[i]}"
    if (( skip )); then skip=0; continue; fi
    case "$word" in
`)
	fmt.Fprintf(&script, "      %s) skip=1 ;;\n", strings.Join(model.valueFlags, "|"))
	script.WriteString(`      -*) ;;
      *) if [[ -z "$resource" ]]; then resource="$word"; elif [[ -z "$action" ]]; then action="$word"; fi ;;
    esac
  done
  if [[ "$cur" == -* ]]; then
`)
	fmt.Fprintf(&script, "    choices=%s\n    case \"$resource $action\" in\n", shellQuote(strings.Join(model.flagNames, " ")))
	writeShellCases(&script, model.operationFlags, "      ")
	script.WriteString(`    esac
  elif [[ -z "$resource" ]]; then
`)
	fmt.Fprintf(&script, "    choices=%s\n", shellQuote(strings.Join(model.commands, " ")))
	script.WriteString(`  elif [[ -z "$action" ]]; then
    case "$resource" in
`)
	writeShellCases(&script, model.actions, "      ")
	script.WriteString(`    esac
  fi
  COMPREPLY=( $(compgen -W "$choices" -- "$cur") )
}
complete -F _cuentica_complete cuentica
`)
	return script.String()
}

func zshCompletion(model completionModel) string {
	var script strings.Builder
	script.WriteString(`#compdef cuentica
_cuentica() {
  local cur="${words[CURRENT]}" prev="${words[CURRENT-1]}"
  local resource="" action="" word choices="" skip=0 i
  case "$prev" in
    --output|-o) compadd -- text json llm; return ;;
    --save|--upload|--key-file) _files; return ;;
`)
	fmt.Fprintf(&script, "    %s) return ;;\n", strings.Join(model.valueFlags, "|"))
	script.WriteString(`  esac
  for (( i=2; i<CURRENT; i++ )); do
    word="${words[i]}"
    if (( skip )); then skip=0; continue; fi
    case "$word" in
`)
	fmt.Fprintf(&script, "      %s) skip=1 ;;\n", strings.Join(model.valueFlags, "|"))
	script.WriteString(`      -*) ;;
      *) if [[ -z "$resource" ]]; then resource="$word"; elif [[ -z "$action" ]]; then action="$word"; fi ;;
    esac
  done
  if [[ "$cur" == -* ]]; then
`)
	fmt.Fprintf(&script, "    choices=%s\n    case \"$resource $action\" in\n", shellQuote(strings.Join(model.flagNames, " ")))
	writeShellCases(&script, model.operationFlags, "      ")
	script.WriteString(`    esac
  elif [[ -z "$resource" ]]; then
`)
	fmt.Fprintf(&script, "    choices=%s\n", shellQuote(strings.Join(model.commands, " ")))
	script.WriteString(`  elif [[ -z "$action" ]]; then
    case "$resource" in
`)
	writeShellCases(&script, model.actions, "      ")
	script.WriteString(`    esac
  fi
  [[ -n "$choices" ]] && compadd -- ${=choices}
}
compdef _cuentica cuentica
`)
	return script.String()
}

func writeShellCases(script *strings.Builder, choices map[string][]string, indent string) {
	for _, key := range sortedCompletionKeys(choices) {
		fmt.Fprintf(script, "%s%s) choices=%s ;;\n", indent, shellQuote(key), shellQuote(strings.Join(choices[key], " ")))
	}
}

func fishCompletion(model completionModel) string {
	var script strings.Builder
	script.WriteString(`# Fish completion for cuentica.
function __cuentica_path
    set -l words (commandline -opc)
    set -e words[1]
    set -l path
    set -l skip 0
    for word in $words
        if test $skip -eq 1
            set skip 0
            continue
        end
        switch $word
`)
	fmt.Fprintf(&script, "            case %s\n                set skip 1\n", fishWords(model.valueFlags))
	script.WriteString(`            case '-*'
            case '*'
                set -a path $word
        end
    end
    string join ' ' -- $path
end

complete -c cuentica -f
`)
	fmt.Fprintf(&script, "complete -c cuentica -n %s -a %s\n", fishQuote(`test -z "$(__cuentica_path)"`), fishQuote(strings.Join(model.commands, " ")))
	for _, command := range sortedCompletionKeys(model.actions) {
		condition := "test \"$(__cuentica_path)\" = " + fishQuote(command)
		fmt.Fprintf(&script, "complete -c cuentica -n %s -a %s\n", fishQuote(condition), fishQuote(strings.Join(model.actions[command], " ")))
	}
	for _, flag := range model.flags {
		fmt.Fprintf(&script, "complete -c cuentica -l %s", fishQuote(flag.name))
		contexts := map[string]bool{"": true}
		supported := 0
		for command, names := range model.operationFlags {
			if slices.Contains(names, "--"+flag.name) {
				supported++
				contexts[command] = true
				contexts[strings.SplitN(command, " ", 2)[0]] = true
			}
		}
		if flag.name == "key-stdin" || flag.name == "key-file" {
			contexts["auth"], contexts["auth login"] = true, true
		}
		if supported < len(model.operationFlags) {
			values := make([]string, 0, len(contexts))
			for value := range contexts {
				values = append(values, value)
			}
			sort.Strings(values)
			condition := "contains -- \"$(__cuentica_path)\" " + fishWords(values)
			fmt.Fprintf(&script, " -n %s", fishQuote(condition))
		}
		if flag.short != "" {
			fmt.Fprintf(&script, " -s %s", fishQuote(flag.short))
		}
		if flag.takesValue {
			script.WriteString(" -r")
		}
		switch flag.name {
		case "output":
			script.WriteString(" -a 'text json llm'")
		case "save", "upload", "key-file":
			script.WriteString(" -F")
		case "issued", "draft", "assigned":
			script.WriteString(" -a 'true false'")
		}
		fmt.Fprintf(&script, " -d %s\n", fishQuote(flag.description))
	}
	return script.String()
}

func powershellCompletion(model completionModel) string {
	var script strings.Builder
	script.WriteString(`# PowerShell completion for cuentica.
Register-ArgumentCompleter -Native -CommandName cuentica -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
`)
	fmt.Fprintf(&script, "    $commands = %s\n    $flags = %s\n    $valueFlags = %s\n", powershellArray(model.commands), powershellArray(model.flagNames), powershellArray(model.valueFlags))
	writePowershellMap(&script, "actions", model.actions)
	writePowershellMap(&script, "operationFlags", model.operationFlags)
	script.WriteString(`    $before = @($commandAst.CommandElements | Select-Object -Skip 1 | Where-Object {
        $_.Extent.EndOffset -lt $cursorPosition -or
        ($_.Extent.EndOffset -eq $cursorPosition -and $_.Extent.Text -ne $wordToComplete)
    } | ForEach-Object { $_.Extent.Text })
    $previous = if ($before.Count -gt 0) { $before[-1] } else { '' }
    $candidates = @()
    if ($previous -in @('--output', '-o')) {
        $candidates = @('text', 'json', 'llm')
    } elseif ($previous -notin $valueFlags) {
        $path = @()
        $skip = $false
        foreach ($word in $before) {
            if ($skip) { $skip = $false; continue }
            if ($word -in $valueFlags) { $skip = $true; continue }
            if ($word.StartsWith('-')) { continue }
            $path += $word
        }
        if ($wordToComplete.StartsWith('-')) {
            $key = $path -join ' '
            $candidates = if ($operationFlags.ContainsKey($key)) { $operationFlags[$key] } else { $flags }
        } elseif ($path.Count -eq 0) {
            $candidates = $commands
        } elseif ($path.Count -eq 1 -and $actions.ContainsKey($path[0])) {
            $candidates = $actions[$path[0]]
        }
    }
    $candidates | Where-Object {
        $_.StartsWith($wordToComplete, [System.StringComparison]::OrdinalIgnoreCase)
    } | Sort-Object -Unique | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`)
	return script.String()
}

func writePowershellMap(script *strings.Builder, name string, values map[string][]string) {
	fmt.Fprintf(script, "    $%s = @{\n", name)
	for _, key := range sortedCompletionKeys(values) {
		fmt.Fprintf(script, "        %s = %s\n", powershellQuote(key), powershellArray(values[key]))
	}
	script.WriteString("    }\n")
}

func sortedCompletionKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fishQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func fishWords(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fishQuote(value)
	}
	return strings.Join(quoted, " ")
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powershellArray(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = powershellQuote(value)
	}
	return "@(" + strings.Join(quoted, ", ") + ")"
}
