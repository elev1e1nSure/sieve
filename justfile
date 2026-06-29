set shell := ["powershell.exe", "-NoProfile", "-Command"]

default:
    just --list

fmt:
    gofmt -w main.go internal

test:
    go test ./...

build:
    go build -o sieve.exe .

run:
    go run .

run-timeout seconds:
    go run . --test-timeout {{seconds}}

release-build:
    New-Item -ItemType Directory -Force dist | Out-Null
    $env:GOOS='windows'; $env:GOARCH='amd64'; go build -o dist/sieve.exe .

clean:
    if (Test-Path sieve.exe) { Remove-Item sieve.exe }
    if (Test-Path dist) { Remove-Item -Recurse -Force dist }
