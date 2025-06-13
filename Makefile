build:
	@go fmt ./cmd ./pkg/client
	@go build -o quicktable ./cmd
	@go install ./pkg/client

clean:
	@rm quicktable

docker:
	@docker build -t ghcr.io/thedevtop/quicktable .
