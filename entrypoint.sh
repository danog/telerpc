#!/bin/sh

git pull

composer update

php server.php serve
