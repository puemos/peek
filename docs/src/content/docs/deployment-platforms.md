---
title: Deployment Platforms
description: Run Peek on cloud platforms without losing SQLite data, uploads, or secrets.
kicker: persistent /data
---

## Rule Of Thumb

Peek is a single-primary service. It stores metadata in SQLite under `PEEK_DATA`, stores the generated signing/encryption key there when `PEEK_SECRET` is not set, and stores uploaded HTML there when `PEEK_STORAGE=file`.

For Docker and cloud platforms, mount durable storage at `/data` and set `PEEK_DATA=/data`. A Docker `VOLUME` declaration does not make a managed platform preserve files by itself; the platform must attach persistent storage at the same path.

Use one running instance against one data directory. S3-compatible storage can move uploaded HTML bytes out of `/data`, but `/data/peek.db` and `/data/secret.key` still need to survive restarts and deploys.

## Base URL

Set `PEEK_BASE_URL` before the first real deployment. Peek uses it for generated share links, first-run setup URLs, OAuth callbacks, CLI login approval URLs, secure cookie behavior, and redirects.

The simplest production path is a custom domain:

```sh
PEEK_BASE_URL=https://peek.example.com
```

On platforms that assign a default hostname, avoid a deploy-redeploy loop by choosing the service name first and setting `PEEK_BASE_URL` to the expected platform URL before the first deploy. If the platform assigns a different URL, update `PEEK_BASE_URL` and redeploy once before inviting users or configuring OAuth.

## Platform Matrix

| Platform | Status | Persistent data setup |
| --- | --- | --- |
| Render | Supported with a persistent disk | Add a persistent disk mounted at `/data`, set `PEEK_DATA=/data`, set `PEEK_BASE_URL` to your custom domain or expected `https://<service-name>.onrender.com`, set health check path `/healthz`, and keep one instance. |
| Fly.io | Supported with a volume | Create a Fly Volume and mount it at `/data`, set `PEEK_DATA=/data`, set `PEEK_BASE_URL` to your Fly hostname or custom domain, and keep one primary Machine for SQLite. |
| Railway | Supported with a volume | Add a Railway volume mounted at `/data`, set `PEEK_DATA=/data`, set `PEEK_BASE_URL` to your Railway or custom domain, and keep one replica. |
| Kubernetes | Supported with a PVC | Mount a `ReadWriteOnce` PersistentVolumeClaim at `/data`, set `PEEK_DATA=/data`, set `PEEK_BASE_URL` to the Ingress URL, and run one replica. |
| Heroku | Not recommended | Heroku dyno filesystems are ephemeral, so local SQLite and file uploads disappear when dynos restart. Peek would need an external SQL metadata backend before this is a good fit. |
| DigitalOcean App Platform | Not recommended | App Platform does not support persistent filesystem volumes. Use a Droplet or Kubernetes instead if you want to run Peek today. |
| AWS App Runner | Not recommended | App Runner storage is ephemeral and stateless. Peek would need an external SQL metadata backend before this is a good fit. |
| Google Cloud Run | Not recommended | Cloud Run instances are stateless and its ephemeral disk persists only for the lifetime of an instance. Peek would need an external SQL metadata backend before this is a good fit. |

## Render

Create a Docker web service from the Peek image or repository. In the service settings, add a persistent disk with mount path `/data`. Only files written under that mount path survive deploys and restarts.

Set environment variables:

```sh
PEEK_DATA=/data
PEEK_BASE_URL=https://peek.example.com
PEEK_TRUSTED_PROXY=true
```

Use your custom domain if you have one. If you are using Render's default domain, choose the service name first and set `PEEK_BASE_URL` to the expected `https://<service-name>.onrender.com` URL before the first deploy. If Render shows a different public URL after creation, update the value and redeploy before configuring OAuth or sharing links.

Set the health check path to `/healthz`. Use one instance because SQLite is the metadata store.

## Fly.io

Create a volume and mount it at `/data`. In `fly.toml`, the mount destination should be `/data`, and the app should set `PEEK_DATA=/data`.

Keep one primary Machine for the app. Fly Volumes are local to Machines, so scaling to multiple Machines does not create one shared SQLite data directory.

## Railway

Add a volume to the service and set its mount path to `/data`. Set `PEEK_DATA=/data` and `PEEK_BASE_URL` to the Railway domain or custom domain you will use.

Keep one replica. If you scale the service horizontally, multiple containers will not safely share one SQLite writer.

## Kubernetes

Use a `Deployment` or `StatefulSet` with one replica and mount a PersistentVolumeClaim at `/data`.

Minimum storage shape:

```yaml
volumeMounts:
  - name: peek-data
    mountPath: /data
volumes:
  - name: peek-data
    persistentVolumeClaim:
      claimName: peek-data
```

Use a `ReadWriteOnce` claim unless your storage class and SQLite operating model have been deliberately tested. Set `PEEK_BASE_URL` to the public Ingress URL and protect `/metrics` at the Ingress or network layer.

## Sources

Platform behavior changes over time, so check the current provider docs before committing to a production setup: [Render persistent disks](https://render.com/docs/disks), [Render web services](https://render.com/docs/web-services), [Fly Volumes](https://fly.io/docs/volumes/overview/), [Railway volumes](https://docs.railway.com/volumes), [Kubernetes Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/), [Heroku dynos](https://devcenter.heroku.com/articles/dynos), [DigitalOcean App Platform storage](https://docs.digitalocean.com/products/app-platform/how-to/store-data/), [AWS App Runner application code](https://docs.aws.amazon.com/apprunner/latest/dg/develop.html), and [Cloud Run ephemeral disk](https://docs.cloud.google.com/run/docs/configuring/services/ephemeral-disk).
