package dynamic_database_config

import (
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/lthibault/jitterbug"
)

type mySQLCredentials struct {
	address  string
	user     string
	password string
	database string
}

func (m *mySQLCredentials) createConnector() (driver.Connector, error) {

	config := mysql.NewConfig()
	config.Addr = m.address
	config.User = m.user
	config.Passwd = m.password
	config.DBName = m.database

	return mysql.NewConnector(config)
}

func TestMySQLDriver(t *testing.T) {
	mySQLHost := os.Getenv("MYSQL_HOST")
	database := "mysqltest1"
	username := "mysqluser1"

	rootDB, err := sql.Open("mysql", "root:password@tcp("+mySQLHost+")/")

	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err := rootDB.Close()
		if err != nil {
			t.Errorf("Unexpected error while closing the mysql connection: %s", err)
		}
	}()

	dropDB := func() {

		_, err = rootDB.Exec("DROP DATABASE IF EXISTS " + database)

		if err != nil {
			t.Errorf("Unexpected error while dropping the mysql database %s: %s", database, err)
		}
	}

	dropDB()

	_, err = rootDB.Exec("CREATE DATABASE " + database)

	if err != nil {
		t.Fatalf("Error creating test mysql database: %s", err)
	}

	defer dropDB()

	dropUser := func() {

		_, err = rootDB.Exec("DROP USER IF EXISTS " + username)

		if err != nil {
			t.Errorf("Unexpected error while dropping the mysql user %s: %s", database, err)
		}
	}

	dropUser()

	password := generatePassword(t)

	_, err = rootDB.Exec("CREATE USER " + username + " IDENTIFIED BY '" + password + "'")

	if err != nil {
		t.Fatalf("Error creating mysql test user: %s", err)
	}

	defer dropUser()

	_, err = rootDB.Exec("GRANT ALL ON " + database + ".test_table TO " + username)

	creds := mySQLCredentials{
		address:  mySQLHost,
		user:     username,
		password: password,
		database: database,
	}

	db := sql.OpenDB(Driver{CreateConnectorFunc: creds.createConnector})

	_, err = db.Exec("CREATE TABLE test_table (id integer not null)")

	if err != nil {
		t.Errorf("Error creating table: %s", err)
	}

	changePasswordTicker := jitterbug.New(1*time.Second, &jitterbug.Norm{Stdev: 2 * time.Second})
	changePasswordCh := make(chan struct{})

	// Change the password randomly
	go func() {
		for range changePasswordTicker.C {
			newPassword := generatePassword(t)

			_, err := rootDB.Exec("SET PASSWORD FOR " + username + " = '" + newPassword + "'")

			if err != nil {
				t.Fatalf("Error updating user password: %s", err)
			}

			processes, err := rootDB.Query("SELECT id FROM information_schema.processlist WHERE user = ?", username)

			if err != nil {
				t.Fatalf("Error querying for processes to terminate: %s", err)
			}

			for processes.Next() {
				var id string

				if err := processes.Scan(&id); err != nil {
					t.Fatalf("Error scanning id: %s", err)
				}

				_, err = rootDB.Exec("KILL " + id)

				if err != nil {
					t.Fatalf("Error killing process: %s", err)
				}
			}

			err = processes.Close()

			if err != nil {
				t.Fatalf("Error closing rows: %s", err)
			}

			creds.password = newPassword
		}

		close(changePasswordCh)
	}()

	queryTicker := jitterbug.New(1*time.Second, &jitterbug.Norm{Stdev: 4 * time.Second})
	defer queryTicker.Stop()

	testDurationTimer := time.NewTimer(3 * time.Minute)
	defer testDurationTimer.Stop()

	insertedRows := 0

terminate:
	for {
		select {
		case <-testDurationTimer.C:
			break terminate

		case <-queryTicker.C:
			_, err := db.Exec("INSERT INTO test_table VALUES(1)")

			if err != nil {
				t.Errorf("Error inserting row: %s", err)
			}

			insertedRows++
		}
	}

	changePasswordTicker.Stop()
	<-changePasswordCh

	var actualInsertedRows int

	err = db.QueryRow("SELECT COUNT(id) FROM test_table").Scan(&actualInsertedRows)

	if err != nil {
		t.Fatalf("Error counting number of inserted rows: %s", err)
	}

	if actualInsertedRows != insertedRows {
		t.Errorf("Inserted rows in database does not match number of insertions, expected %d, got %d", insertedRows, actualInsertedRows)
	}
}

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

func TestPostgresDriver(t *testing.T) {
	postgresHost := os.Getenv("POSTGRES_HOST")
	database := "libpqtest1"
	username := "libpquser1"

	rootDB, err := sql.Open("postgres", "postgres://postgres:password@"+postgresHost+"/?sslmode=disable")

	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		err := rootDB.Close()
		if err != nil {
			t.Errorf("Unexpected error while closing the postgres connection: %s", err)
		}
	}()

	dropDB := func() {

		_, err = rootDB.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", database)

		if err != nil {
			t.Fatalf("Error terminating connections to database: %s", err)
		}

		_, err = rootDB.Exec("DROP DATABASE IF EXISTS " + database)

		if err != nil {
			t.Errorf("Unexpected error while dropping the postgres database %s: %s", database, err)
		}
	}

	dropDB()

	_, err = rootDB.Exec("CREATE DATABASE " + database)

	if err != nil {
		t.Fatalf("Error creating test postgres database: %s", err)
	}

	defer dropDB()

	dropUser := func() {
		_, err = rootDB.Exec("DROP USER IF EXISTS " + username)

		if err != nil {
			t.Errorf("Unexpected error while dropping the postgres user %s: %s", database, err)
		}
	}

	dropUser()

	password := generatePassword(t)

	_, err = rootDB.Exec("CREATE USER " + username + " WITH PASSWORD '" + password + "'")

	if err != nil {
		t.Fatalf("Error creating postgres test user: %s", err)
	}

	defer dropUser()

	_, err = rootDB.Exec("GRANT ALL PRIVILEGES ON test_table IN SCHEMA" + database + " TO " + username)

	creds := postgresCredentials{
		host:     postgresHost,
		username: username,
		password: password,
		database: database,
	}

	db := sql.OpenDB(Driver{CreateConnectorFunc: creds.createConnector})

	_, err = db.Exec("CREATE TABLE test_table (id integer not null)")

	if err != nil {
		t.Errorf("Error creating table: %s", err)
	}

	defer func() {
		_, err = db.Exec("DROP TABLE IF EXISTS test_table")

		if err != nil {
			t.Errorf("Error dropping table: %s", err)
		}
	}()

	changePasswordTicker := jitterbug.New(1*time.Second, &jitterbug.Norm{Stdev: 2 * time.Second})
	changePasswordCh := make(chan struct{})

	// Change the password randomly
	go func() {
		for range changePasswordTicker.C {
			newPassword := generatePassword(t)

			_, err := rootDB.Exec("ALTER USER " + username + " WITH PASSWORD '" + newPassword + "'")

			if err != nil {
				t.Fatalf("Error updating user password: %s", err)
			}

			_, err = rootDB.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = $1", username)

			if err != nil {
				t.Fatalf("Error terminating user connection: %s", err)
			}

			creds.password = newPassword
		}

		close(changePasswordCh)
	}()

	queryTicker := jitterbug.New(1*time.Second, &jitterbug.Norm{Stdev: 4 * time.Second})
	defer queryTicker.Stop()

	testDurationTimer := time.NewTimer(3 * time.Minute)
	defer testDurationTimer.Stop()

	insertedRows := 0

terminate:
	for {
		select {
		case <-testDurationTimer.C:
			break terminate

		case <-queryTicker.C:
			_, err := db.Exec("INSERT INTO test_table VALUES(1)")

			if err != nil {
				t.Errorf("Error inserting row: %s", err)
			}

			insertedRows++
		}
	}

	changePasswordTicker.Stop()
	<-changePasswordCh

	var actualInsertedRows int

	err = db.QueryRow("SELECT COUNT(id) FROM test_table").Scan(&actualInsertedRows)

	if err != nil {
		t.Fatalf("Error counting number of inserted rows: %s", err)
	}

	if actualInsertedRows != insertedRows {
		t.Errorf("Inserted rows in database does not match number of insertions, expected %d, got %d", insertedRows, actualInsertedRows)
	}
}

func generatePassword(t *testing.T) string {
	dictionary := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	var bytes = make([]byte, 30)
	_, err := rand.Read(bytes)

	if err != nil {
		t.Fatalf("Error generating password: %s", err)
	}

	for k, v := range bytes {
		bytes[k] = dictionary[v%byte(len(dictionary))]
	}

	return string(bytes)
}
