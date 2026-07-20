# Contacts

A lightweight, self-hosted contacts and household manager with Excel synchronization and configurable PDF label generation.

Contacts is built with Go, Htmx and SQLite. It is intended for small-scale or single-user contact management.

## Features

* Manage individual contacts and households
* Store shared addresses, telephone numbers and email addresses per household
* Organize contacts using tags
* Filter contacts and households by name, address, city, birth year and tags
* Export contacts to Excel and synchronize changes back from Excel
* Configure label sheets, label layouts and print filters
* Generate address labels and checklists as PDF files
* Store all application data in a single persistent SQLite data directory
* Create consistent database backups from within the application

## Important security notice

Contacts currently has no built-in authentication.

Do not expose port `8080` directly to the public internet. Run the application only on a trusted local network or place it behind a reverse proxy that provides authentication and HTTPS.

## Quick start with Docker Compose

Create a directory for Contacts and add the following `compose.yaml` file:

```yaml
services:
  contacts:
    image: paulpeeters/contacts:latest
    container_name: contacts
    restart: unless-stopped

    ports:
      - "8080:8080"

    environment:
      TZ: Europe/Brussels

    volumes:
      - ./data:/app/data
```

Start the container:

```bash
docker compose up -d
```

Open the application in your browser:

```text
http://localhost:8080
```

To use another host port, change only the left-hand side of the port mapping. For example:

```yaml
ports:
  - "9090:8080"
```

The application will then be available at:

```text
http://localhost:9090
```

## Docker run

The same container can be started without Docker Compose:

```bash
docker run -d \
  --name contacts \
  --restart unless-stopped \
  -p 8080:8080 \
  -e TZ=Europe/Brussels \
  -v contacts-data:/app/data \
  paulpeeters/contacts:latest
```

## Persistent data

The container stores its persistent files in:

```text
/app/data
```

This directory contains files such as:

* `contacts.db` — SQLite database
* `appsettings.json` — application settings
* `contacts.log` — application log

Always mount `/app/data` as a bind mount or named volume. Removing the container without persistent storage will otherwise remove the application data with it.

## Configuration

| Setting                | Default           | Description                                                   |
| ---------------------- | ----------------- | ------------------------------------------------------------- |
| `TZ`                   | `Europe/Brussels` | Time zone used for logging and backup filenames               |
| `CONTACTS_LISTEN_HOST` | `0.0.0.0`         | Address on which the application listens inside the container |

The default container port is `8080`.

## Platform

The currently published image is built for:

```text
linux/amd64
```

## Source code and support

* [Source code on GitHub](https://github.com/paulpeeters/contacts)
* [Report a problem](https://github.com/paulpeeters/contacts/issues)
* [Full project documentation](https://github.com/paulpeeters/contacts#readme)

## License

Contacts is licensed under the [GNU Affero General Public License v3.0 only](https://github.com/paulpeeters/contacts/blob/main/LICENSE) (`AGPL-3.0-only`).

Copyright © 2026 Paul Peeters.

The complete source code is available on [GitHub](https://github.com/paulpeeters/contacts).

Third-party libraries and components included in the application remain subject to their respective licenses.
