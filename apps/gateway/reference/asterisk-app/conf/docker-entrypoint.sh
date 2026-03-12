#!/bin/bash

#Set ENV
sed -i "s/\$amp_conf\['AMPDBUSER'\] = .*/\$amp_conf\['AMPDBUSER'\] = '$DB_USER';/g" /etc/freepbx.conf
sed -i "s/\$amp_conf\['AMPDBPASS'\] = .*/\$amp_conf\['AMPDBPASS'\] = '$DB_PASS';/g" /etc/freepbx.conf
sed -i "s/\$amp_conf\['AMPDBHOST'\] = .*/\$amp_conf\['AMPDBHOST'\] = '$DB_IP';/g" /etc/freepbx.conf
sed -i "s/AMPMGRPASS=.*/AMPMGRPASS=$AMI_PASS/" /etc/amportal.conf
sed -i "s/AMPDBUSER=.*/AMPDBUSER=$DB_USER/" /etc/amportal.conf
sed -i "s/AMPDBPASS=.*/AMPDBPASS=$DB_PASS/" /etc/amportal.conf
sed -i "s/AMPDBHOST=.*/AMPDBHOST=$DB_IP/" /etc/amportal.conf
sed -i "s/secret = .*/secret = $AMI_PASS/" /etc/asterisk/manager.conf
sed -i "s/dbuser = .*/dbuser = $DB_USER/" /etc/asterisk/res_config_mysql.conf
sed -i "s/dbpass = .*/dbpass = $DB_PASS/" /etc/asterisk/res_config_mysql.conf
sed -i "s/user=.*/user=$DB_USER/" /etc/asterisk/cdr_mysql.conf
sed -i "s/password=.*/password=$DB_PASS/" /etc/asterisk/cdr_mysql.conf
sed -i "s/upload_max_filesize = .*/upload_max_filesize = 120M/" /etc/php/5.6/fpm/php.ini
sed -i "s/memory_limit = .*/memory_limit = 512M/" /etc/php/5.6/fpm/php.ini

ln -s /usr/bin/vim.tiny /usr/bin/vim 
#Set chown
chown -R asterisk:asterisk /etc/asterisk /var/lib/asterisk /var/log/asterisk /var/spool/asterisk /var/www/html /run/asterisk /etc/freepbx.conf /etc/amportal.conf

#init start asterisk, freepbx
fwconsole chown
fwconsole start
fwconsole reload

#start nginx, php
service nginx restart
service php5.6-fpm start

#freepbx upgrade
#fwconsole ma upgradeall

fwconsole dbug &

sleep infinity
