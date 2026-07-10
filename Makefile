build:
	@go fmt ./cmd
	@go build -o quicktable ./cmd

clean:
	@rm quicktable
