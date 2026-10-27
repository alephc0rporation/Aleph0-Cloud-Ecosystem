-- Dev only. Runs once, when the data volume is empty.
-- Production uses Cloud SQL with its own users and secrets.
CREATE USER api WITH PASSWORD 'api';
CREATE DATABASE api OWNER api;

CREATE USER keycloak WITH PASSWORD 'keycloak';
CREATE DATABASE keycloak OWNER keycloak;

-- Only each database's owner may connect to it.
REVOKE CONNECT ON DATABASE api FROM PUBLIC;
REVOKE CONNECT ON DATABASE keycloak FROM PUBLIC;
