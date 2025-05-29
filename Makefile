build:
	@go fmt ./cmd
	@go build -o quicktable ./cmd

clean:
	@rm quicktable

docker:
	@docker build -t ghcr.io/thedevtop/quicktable .
