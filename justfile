set shell := ["powershell.exe", "-NoProfile", "-Command"]

default:
    just --list

fmt:
    gofmt -w main.go internal

fmt-check:
    $files = gofmt -l main.go internal; if ($files) { Write-Output $files; exit 1 }

lint:
    go vet ./...
    $env:PATH = "$(go env GOPATH)\bin;$env:PATH"; golangci-lint run

check: fmt-check lint test build

icon:
    go-winres simply --arch amd64 --icon assets/icon/icon.png --manifest cli --file-description "sieve" --product-name "sieve" --copyright "elev1e1nSure" --product-version "git-tag" --file-version "git-tag"

test:
    go test ./...

build:
    $version = if ($env:VERSION) { $env:VERSION } else { 'dev' }; $commit = git rev-parse --short HEAD; $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ'); go build -ldflags "-s -w -X github.com/elev1e1nSure/sieve/internal/version.Version=$version -X github.com/elev1e1nSure/sieve/internal/version.Commit=$commit -X github.com/elev1e1nSure/sieve/internal/version.Date=$date" -o sieve.exe .

run:
    go run .

run-timeout seconds:
    go run . --test-timeout {{seconds}}

release-build:
    New-Item -ItemType Directory -Force dist | Out-Null
    $version = if ($env:VERSION) { $env:VERSION } else { git describe --tags --abbrev=0 }; $commit = git rev-parse --short HEAD; $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ'); $env:GOOS='windows'; $env:GOARCH='amd64'; go build -ldflags "-s -w -X github.com/elev1e1nSure/sieve/internal/version.Version=$version -X github.com/elev1e1nSure/sieve/internal/version.Commit=$commit -X github.com/elev1e1nSure/sieve/internal/version.Date=$date" -o dist/sieve-windows-amd64.exe .

clean:
    if (Test-Path sieve.exe) { Remove-Item sieve.exe }
    if (Test-Path dist) { Remove-Item -Recurse -Force dist }
