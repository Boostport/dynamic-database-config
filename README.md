# Dynamic Config Wrapper Driver for Go's database/sql Package

[![PkgGoDev](https://pkg.go.dev/badge/github.com/Boostport/dynamic-database-config)](https://pkg.go.dev/github.com/Boostport/dynamic-database-config)
[![Tests Status](https://github.com/Boostport/dynamic-database-config/workflows/Test/badge.svg)](https://github.com/Boostport/dynamic-database-config)
[![Test Coverage](https://api.codeclimate.com/v1/badges/d453f2dc545b3f630aad/test_coverage)](https://codeclimate.com/github/Boostport/dynamic-database-config/test_coverage)

## Why do we need this?
Go's [database/sql](https://golang.org/pkg/database/sql/) does a lot of heavy lifting under the hood to abstract away
connection pooling and other low-level operations.

However, if you are rotating credentials for your databases in production, it is not possible to update the
configuration for an existing [`sql.DB`](https://golang.org/pkg/database/sql/#DB) object to use these new credentials.

For example, you might use one of the following to rotate credentials every x hours or minutes:
- Vault's [database secret engine](https://www.vaultproject.io/docs/secrets/databases)
- AWS's [IAM for RDS](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAMDBAuth.html) which rotates the authentication token every 15 minutes

When the current credentials that are used to create the underlying connections in `sql.DB` are revoked, operations on
`sql.DB` will fail. As `sql.DB` is concurrently safe, the recommendation is to create a `sql.DB` in the entrypoint
of your application and pass that instance down to where it needs to be used. Because of this, it is not possible to update
the connection parameters or DSN of a `sql.DB` instance once it has been created.

This package is a simple `database/sql` driver that wraps the underlying driver you want to use by instantiating new 
connections for `sql.DB` via delegation to the underlying driver's [`Connector`](https://golang.org/pkg/database/sql/driver/#Connector).

When database credentials are revoked and the current session or connection to the database server is terminated, `database/sql`
drivers should return [driver.ErrBadConn](https://golang.org/pkg/database/sql/driver/#pkg-variables). This signals to `sql.DB` that
the connection should be removed from the pool and a new one should be created. By using this driver, you can specify a 
[`CreateConnectorFunc`](https://pkg.go.dev/github.com/Boostport/dynamic-database-config/#CreateConnectorFunc) to create a new
connection with the new credentials by returning a new Connector from your driver.

## Prerequisites
- The database driver you want to use needs to support the [`driver.Connector`](https://golang.org/pkg/database/sql/driver/#Connector)
interface.
- The database driver should return [driver.ErrBadConn](https://golang.org/pkg/database/sql/driver/#pkg-variables) when the
connection to the database is terminated.
- The tool / service that you use to rotate database credentials should terminate the session or connection upon
revocation of the old credentials. For MySQL and PostgreSQL, current sessions are not terminated when the user account
is dropped or has its password updated, therefore the current connections can still perform some operations on the database
and operate in "limbo".

## Implementing your own `CreateConnectorFunc`
`CreateConnectorFunc` is simply a function that returns a `driver.Connector` and an error. It is up to you how you want to
implement this. There are some examples for MySQL and PostgreSQL in the [tests](driver_test.go).

As a quick example for PostgreSql:
```go
type postgresCredentials struct {
	host     string
	username string
	password string
	database string
}

func (p *postgresCredentials) createConnector() (driver.Connector, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", p.username, p.password, p.host, p.database)
	return pq.NewConnector(dsn)
}

creds := postgresCredentials{
    host:     "localhost",
    username: "dynamically-generated-username",
    password: "dynamically-generated-password",
    database: "database",
}

db := sql.OpenDB(Driver{CreateConnectorFunc: creds.createConnector})

// Use db as you'd normally do so here.
```

*Tip:* Avoid making expensive network calls in your `CreateConnectorFunc` as it is called everytime a new connection is
created. Instead, update and cache the credentials in a data structure and use the values from there when creating
a new connection.

## Do I need to import this library?
The code for the wrapper is only a few lines in [driver.go](driver.go), so you can just copy it into your project.
However, this is being maintained as a library because we have tests in place to make sure it works correctly
for future releases of Go and other databases.

## Is there a better way to do this?
Ideally, `sql.DB` would allow users to implement an interceptor function to customise the connector during creation.
In addition, being able to signal `sql.DB` to gracefully shutdown current connections would be a huge plus.

## Errors in MySQL tests
When running the MySQL tests, the following error is written to stdout: `[mysql] 2020/12/02 23:16:09 packets.go:122: closing bad idle connection: EOF`
This happens we case we terminated the connection to the server when revoking the credentials. It does not seem to be harmful
in this case.

## References
- [How to connect to RDS Postgres using IAM Authentication in Golang](https://github.com/califlower/golang-aws-rds-iam-postgres)
- [Include code to easily connect RDS IAM authentication to a database.sql.DB](https://github.com/aws/aws-sdk-go/issues/3043)