#!/bin/sh

git pull

composer update

php server.php $1
