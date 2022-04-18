run:
	go run cmd/main.go
build:
	go build -o bin/main cmd/main.go
compile:
	GOOS=windows GOARCH=amd64 go build -o bin/main-windows64 cmd/main.go
	GOOS=windows GOARCH=386 go build -o bin/main-windows386 cmd/main.go
	GOOS=darwin GOARCH=amd64 go build -o bin/main-mac64 cmd/main.go
	GOOS=linux GOARCH=386 go build -o bin/main-linux386 cmd/main.go
	GOOS=linux GOARCH=amd64 go build -o bin/main-linux64 cmd/main.go
