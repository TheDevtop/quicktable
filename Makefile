build:
	@go fmt ./cmd ./pkg/client
	@go build -o quicktable ./cmd
	@go install ./pkg/client

clean:
	@rm quicktable
