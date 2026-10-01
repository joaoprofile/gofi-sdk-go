#!/bin/sh
awslocal sqs create-queue --queue-name orders
awslocal sqs create-queue --queue-name orders-dlq
