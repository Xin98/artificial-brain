// Package policy validates the repository's dependency direction.
package policy

import (
	"bufio"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Violation identifies one forbidden dependency.
type Violation struct {
	File   string
	Rule   string
	Import string
}

var moduleDeclaration = regexp.MustCompile(`^module\s+([^\s]+)\s*$`)

// Validate returns every architecture-policy violation below root in stable order.
func Validate(root string) ([]Violation, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	modulePath := findModulePath(absRoot)
	includeTestdata := isFixturePath(absRoot)
	files, err := sourceFiles(absRoot, includeTestdata)
	if err != nil {
		return nil, err
	}

	violations := make([]Violation, 0)
	for _, file := range files {
		rel, err := filepath.Rel(absRoot, file)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		switch filepath.Ext(file) {
		case ".go":
			found, err := validateGo(file, rel, modulePath)
			if err != nil {
				return nil, err
			}
			violations = append(violations, found...)
		case ".ts", ".tsx":
			found, err := validateTypeScript(file, rel)
			if err != nil {
				return nil, err
			}
			violations = append(violations, found...)
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Rule != violations[j].Rule {
			return violations[i].Rule < violations[j].Rule
		}
		return violations[i].Import < violations[j].Import
	})
	return violations, nil
}

func sourceFiles(root string, includeTestdata bool) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == ".next" || name == "dist" || name == "build" || name == "coverage" || (!includeTestdata && name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		if extension == ".go" || extension == ".ts" || extension == ".tsx" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func validateGo(file, relativePath, modulePath string) ([]Violation, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	context, inModule := moduleFromPath(relativePath)
	var violations []Violation
	for _, spec := range parsed.Imports {
		importPath := strings.Trim(spec.Path.Value, "\"")
		if isDomainFile(relativePath) && !isStandardLibrary(importPath) && !isOwnDomainImport(importPath, modulePath, context) {
			violations = append(violations, Violation{relativePath, "go-domain-dependency", importPath})
		}
		if isApplicationFile(relativePath) && isApplicationForbidden(importPath) {
			violations = append(violations, Violation{relativePath, "go-application-dependency", importPath})
		}
		if inModule && importsOtherContext(importPath, context) {
			violations = append(violations, Violation{relativePath, "go-cross-context", importPath})
		}
		if isPlatformFile(relativePath) && hasPathSegments(importPath, "backend", "internal", "modules") {
			violations = append(violations, Violation{relativePath, "go-platform-dependency", importPath})
		}
	}
	return violations, nil
}

func isStandardLibrary(importPath string) bool {
	return isStandardLibraryWithContext(build.Default, importPath)
}

func isStandardLibraryWithContext(context build.Context, importPath string) bool {
	if importPath == "C" {
		return true
	}
	pkg, err := context.Import(importPath, "", build.FindOnly)
	return err == nil && pkg.Goroot
}

func isOwnDomainImport(importPath, modulePath, context string) bool {
	if modulePath == "" || context == "" {
		return false
	}
	base := modulePath + "/backend/internal/modules/" + context + "/domain"
	return importPath == base || strings.HasPrefix(importPath, base+"/")
}

func isApplicationForbidden(importPath string) bool {
	lower := strings.ToLower(importPath)
	return strings.Contains(importPath, "/adapters/") ||
		strings.Contains(importPath, "/platform/database") ||
		strings.Contains(importPath, "/platform/server") ||
		importPath == "net/http" ||
		strings.Contains(lower, "pgx") ||
		strings.Contains(lower, "river") ||
		strings.Contains(lower, "openai") ||
		strings.Contains(lower, "smtp") ||
		strings.Contains(lower, "sms") ||
		strings.Contains(lower, "twilio") ||
		strings.Contains(lower, "vonage")
}

func importsOtherContext(importPath, context string) bool {
	importedContext, found := moduleFromPath(importPath)
	if !found || importedContext == context {
		return false
	}
	return strings.Contains("/"+strings.Trim(importPath, "/")+"/", "/domain/") || strings.Contains("/"+strings.Trim(importPath, "/")+"/", "/adapters/")
}

func moduleFromPath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == "modules" && parts[index+1] != "" {
			return parts[index+1], true
		}
	}
	return "", false
}

func isDomainFile(path string) bool      { return hasPathSegments(path, "domain") }
func isApplicationFile(path string) bool { return hasPathSegments(path, "application") }
func isPlatformFile(path string) bool {
	return hasPathSegments(path, "backend", "internal", "platform")
}

func hasPathSegments(path string, wanted ...string) bool {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	for start := 0; start+len(wanted) <= len(parts); start++ {
		matches := true
		for offset, want := range wanted {
			if parts[start+offset] != want {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func validateTypeScript(file, relativePath string) ([]Violation, error) {
	if !hasPathSegments(relativePath, "apps", "web", "src", "features") {
		return nil, nil
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	code := stripComments(string(contents))
	var violations []Violation
	if strings.Contains(code, "process.env.API_INTERNAL_URL") {
		violations = append(violations, Violation{relativePath, "web-feature-env", "process.env.API_INTERNAL_URL"})
	} else if strings.Contains(code, "process.env") {
		violations = append(violations, Violation{relativePath, "web-feature-env", "process.env"})
	} else if strings.Contains(code, "API_INTERNAL_URL") {
		violations = append(violations, Violation{relativePath, "web-feature-env", "API_INTERNAL_URL"})
	}
	for _, match := range composeHostReference.FindAllString(code, -1) {
		violations = append(violations, Violation{relativePath, "web-feature-env", cleanComposeHostReference(match)})
	}
	for _, match := range standaloneComposeHostAssignment.FindAllStringSubmatch(code, -1) {
		violations = append(violations, Violation{relativePath, "web-feature-env", match[1]})
	}
	for _, imported := range typeScriptImports(code) {
		if strings.Contains(strings.ReplaceAll(imported, "\\", "/"), "src/shared/server") || strings.Contains(imported, "shared/server") {
			violations = append(violations, Violation{relativePath, "web-feature-env", imported})
		}
	}
	return violations, nil
}

var typeScriptImport = regexp.MustCompile(`(?m)\b(?:import|export)\s+(?:[^;]*?\s+from\s+)?["']([^"']+)["']`)

var composeHostReference = regexp.MustCompile("(?i:(?:https?://(?:[^/?#\\s@]+@)?(?:api|worker|postgres|db)(?::(?:[0-9]+|\\$\\{[^}\\r\\n]+\\}))?(?:[/?#]|$|[\\s\\\"'`,;)])|postgres://(?:[^/?#\\s@]+@)?(?:api|worker|postgres|db)(?::(?:[0-9]+|\\$\\{[^}\\r\\n]+\\}))?(?:[/?#]|$|[\\s\\\"'`,;)])|\\b(?:api|worker|postgres|db):(?:[0-9]+|\\$\\{[^}\\r\\n]+\\})(?:$|[\\s\\\"'`,;)])))")

var standaloneComposeHostAssignment = regexp.MustCompile("(?m)=\\s*[\\\"'`](api|worker|postgres|db)[\\\"'`]\\s*;")

func cleanComposeHostReference(reference string) string {
	return strings.TrimRight(reference, " \t\r\n\"'`,;)")
}

func typeScriptImports(code string) []string {
	matches := typeScriptImport.FindAllStringSubmatch(code, -1)
	imports := make([]string, 0, len(matches))
	for _, match := range matches {
		imports = append(imports, match[1])
	}
	return imports
}

func stripComments(code string) string {
	var result strings.Builder
	result.Grow(len(code))
	for index := 0; index < len(code); {
		if code[index] == '/' && index+1 < len(code) && code[index+1] == '/' {
			for index < len(code) && code[index] != '\n' {
				result.WriteByte(' ')
				index++
			}
			continue
		}
		if code[index] == '/' && index+1 < len(code) && code[index+1] == '*' {
			result.WriteString("  ")
			index += 2
			for index+1 < len(code) && !(code[index] == '*' && code[index+1] == '/') {
				if code[index] == '\n' {
					result.WriteByte('\n')
				} else {
					result.WriteByte(' ')
				}
				index++
			}
			if index+1 < len(code) {
				result.WriteString("  ")
				index += 2
			}
			continue
		}
		quote := code[index]
		result.WriteByte(quote)
		index++
		if quote != '\'' && quote != '"' && quote != '`' {
			continue
		}
		for index < len(code) {
			character := code[index]
			result.WriteByte(character)
			index++
			if character == '\\' && index < len(code) {
				result.WriteByte(code[index])
				index++
				continue
			}
			if character == quote {
				break
			}
		}
	}
	return result.String()
}

func isFixturePath(path string) bool {
	return hasPathSegments(path, "testdata")
}

func findModulePath(root string) string {
	for directory := root; ; directory = filepath.Dir(directory) {
		file, err := os.Open(filepath.Join(directory, "go.mod"))
		if err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				match := moduleDeclaration.FindStringSubmatch(scanner.Text())
				if len(match) == 2 {
					_ = file.Close()
					return match[1]
				}
			}
			_ = file.Close()
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
	}
}
