CREATE TABLE probe_result(
    id BIGINT  PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(128) NOT NULL,
    success BOOLEAN NOT NULL,
    latency_us INT NOT NULL,
    status_code INT NOT NULL DEFAULT 0,
    err_msg VARCHAR(256)   NULL,
    tls_not_after DATETIME(3)  NULL,
    checked_at DATETIME(3) NOT NULL,
    KEY idx_name_time (name, checked_at)
) ENGINE=InnoDB
