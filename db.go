package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
)

const (
	CreateTableSqlQuery = `
		create table if not exists main.threshold
		(
			id                 integer primary key autoincrement,
			service_id         TEXT unique,
			low_mem_threshold  integer,
			high_mem_threshold integer
		);
	`
	GetServiceInfoByServiceIdSqlQuery = `SELECT id, service_id, low_mem_threshold, high_mem_threshold FROM main.threshold 
						      Where service_id = ?`
	GetAllServicesInfoSqlQuery = "SELECT * FROM main.threshold"
)

type SqliteDB struct {
	db *sql.DB
}

func initializeDB(conn *sql.DB) error {
	_, err := conn.Exec(CreateTableSqlQuery)
	if err != nil {
		return err
	}

	return nil
}

func NewSqliteDB(dsn string) *SqliteDB {
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		panic(err)
	}
	if pingErr := conn.Ping(); pingErr != nil {
		panic(pingErr)
	}

	initErr := initializeDB(conn)
	if initErr != nil {
		log.Panicln("can't initialize database: ", initErr)
	}
	log.Println("db connection successful")
	return &SqliteDB{
		db: conn,
	}
}

type Threshold struct {
	ID               int64
	ServiceID        string
	LowMemThreshold  int
	HighMemThreshold int
}

func (d *SqliteDB) GetServiceInfo(serviceId string) (Threshold, error) {
	row := d.db.QueryRow(GetServiceInfoByServiceIdSqlQuery, serviceId)
	if row.Err() != nil {
		return Threshold{}, errors.New(fmt.Sprintf("ERR GetServiceInfo: %w", row.Err()))
	}
	threshold := Threshold{}
	err := row.Scan(&threshold.ID, &threshold.ServiceID, &threshold.LowMemThreshold, &threshold.HighMemThreshold)
	if err != nil {
		if err == sql.ErrNoRows {
			return Threshold{}, errors.New("not found")
		}

		return Threshold{}, errors.New(fmt.Sprintf("ERR GetServiceInfo Scan: %s", row.Err()))
	}

	return threshold, nil
}

func (d *SqliteDB) GetAllServices() ([]Threshold, error) {
	rows, err := d.db.Query(GetAllServicesInfoSqlQuery)
	if err != nil {
		if err == sql.ErrNoRows {
			return []Threshold{}, errors.New("not found")
		}

		return []Threshold{}, nil
	}

	var servicesList []Threshold
	for rows.Next() {
		service := Threshold{}
		err = rows.Scan(&service.ID, &service.ServiceID, &service.LowMemThreshold, &service.HighMemThreshold)
		if err != nil {
			log.Println("GetAllServices - err scan: ", err)

			continue
		}
		servicesList = append(servicesList, service)
	}

	return servicesList, nil
}

func (d *SqliteDB) InsertThreshold(threshold Threshold) error {
	_, err := d.db.Exec(
		"INSERT INTO main.threshold(service_id, low_mem_threshold, high_mem_threshold) VALUES (?, ?, ?)",
		threshold.ServiceID, threshold.LowMemThreshold, threshold.HighMemThreshold,
	)
	if err != nil {
		return err
	}

	return nil
}
