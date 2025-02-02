#!/bin/sh

set +e

gcloud functions deploy gcp-monitoring-to-discord \
    --entry-point=F \
    --memory=256MB \
    --region=us-central1 \
    --runtime=go123 \
    --env-vars-file=.env.yaml \
    --trigger-http \
    --timeout=10s

