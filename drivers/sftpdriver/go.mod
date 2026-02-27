module github.com/xraph/trove/drivers/sftpdriver

go 1.25.7

require (
	github.com/pkg/sftp v1.13.7
	github.com/stretchr/testify v1.11.1
	github.com/xraph/trove v0.0.0
	golang.org/x/crypto v0.37.0
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	golang.org/x/sys v0.32.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/xraph/trove => ../..
