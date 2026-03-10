FROM ubuntu:18.04 As baseimage
LABEL maintainer="maitree.e@spinsoft.co.th"

RUN apt-get update \
	&& ln -s /usr/share/zoneinfo/Asia/Bangkok /etc/localtime \
	&& apt-get install -y --no-install-recommends \
	nginx-light \
	ca-certificates \
	mpg123 \
	sox \
	uuid \
	vim-tiny \
	cron  \
	libiksemel3 \
	libpjsua2 \
	libjansson4 \
	openssl \
	lua5.1 \
	libnewt0.52 \
	libsqlite3-0 \
	libxslt1.1 \
	libpq5 \
	libgmime-2.6-0 \
	liblua5.1-0  \
	libneon27 \
	libsnmp30 \
	libspandsp2 \
	libpci3 \
	libglib2.0-0 \
	libsdl1.2debian \
	libsybdb5 \
	gnupg2 \
	curl \
	wget \
	tzdata \
	libmysqlclient-dev \
	&& echo "deb https://debian.octopuce.fr/snapshots/sury-php/20220630/ stretch main" | tee /etc/apt/sources.list.d/php.list \
    && wget -qO - https://debian.octopuce.fr/sury-php/apt.gpg | apt-key add - \
	&& apt update \
	&& curl -sL https://deb.nodesource.com/setup_8.x | bash - \
	&& apt-get install -y \
	nodejs \
	python3.7 \
    python-pip \
	libcurl3 \
	php-common \
	php-pear \
	php5.6-cli \
	php5.6-common \
	php5.6-fpm \
	php5.6-json \
	php5.6-mysql \
	php5.6-opcache \
	php5.6-readline \
	php5.6-xml \
	php5.6-xmlrpc \
	php5.6-curl \
	php5.6-mbstring \
	&& pip install apns \
        && pip install pyst2 \
	&& apt-get clean -y \
	&& rm -fr /var/lib/apt/lists/* \
	&& adduser --gecos ,,,,, --disabled-password asterisk

FROM baseimage As builder

RUN export DEP="build-essential \
	pkg-config \
	automake \
	libtool \
	autoconf \
	git \
	subversion \
	wget \
	bison \
	flex \
	libpjproject-dev \
	libncurses-dev \
	libz-dev \
	libssl-dev \
	libxml2-dev \
	libsqlite3-dev \
	uuid-dev \
	libspeex-dev \
	libspeexdsp-dev \
	libogg-dev \
	libvorbis-dev \
	libasound2-dev \
	portaudio19-dev \
	unixodbc-dev \
	libpq-dev \
	libneon27-dev \
	libgmime-2.6-dev \
	libusb-dev \
	liblua5.1-0-dev \
	libvpb-dev \
	libgtk2.0-dev \
	libmysqlclient-dev \
	libbluetooth-dev \
	freetds-dev \
	libsnmp-dev \
	libiksemel-dev \
	libnewt-dev \
	libpopt-dev \
	libical-dev \
	libspandsp-dev \
	libjack-dev \
	libresample-dev \
	libc-client-dev \
	binutils-dev \
	libsrtp-dev \
	libgsm1-dev \
	libedit-dev \
	doxygen \
	libjansson-dev \
	libldap-dev \
	libxslt1-dev" \
	&& apt-get update \
	&& DEBIAN_FRONTEND=noninteractive apt-get -y install $DEP 

RUN echo "force build #2"

RUN mkdir /build \
	&& cd /build \
	&& git clone --single-branch --branch master_vrs_asterisk_15 https://github.com/spinsoft-ttrs/ttrs_ippbx_asterisk_source_15.git asterisk_source \
	&& cd /build/asterisk_source \
	# && yes |contrib/scripts/install_prereq install \
	&& PTLIB_CONFIG=/usr/share/ptlib/make/ptlib-config ./configure \
	&& make menuselect.makeopts \
	&& menuselect/menuselect --enable res_config_mysql menuselect.makeopts \
	&& menuselect/menuselect --enable app_mysql menuselect.makeopts \
	&& menuselect/menuselect --enable cdr_mysql menuselect.makeopts \
	&& menuselect/menuselect --enable codec_opus menuselect.makeopts \
	&& menuselect/menuselect --enable astman menuselect.makeopts \
	&& menuselect/menuselect --disable BUILD_NATIVE menuselect.makeopts \
	&& menuselect/menuselect --enable EXTRA-SOUNDS-EN-WAV menuselect.makeopts \
	&& make \
	&& make install \
	&& make config \
	&& make samples \
	&& ldconfig \
	&& sed -r -e 's/\[directories\]\(!\)/[directories]/' -i /etc/asterisk/asterisk.conf \
	&& chown -R asterisk:asterisk /etc/asterisk /var/lib/asterisk /var/log/asterisk /var/spool/asterisk /run/asterisk
RUN cd /build \
	&& wget http://mirror.freepbx.org/modules/packages/freepbx/freepbx-15.0-latest.tgz  \
	&& tar -xzf freepbx-15.0-latest.tgz \
	&& rm -f freepbx-15.0-latest.tgz
RUN apt-get clean \
	&& apt-get -y purge mysql* \
	&& apt-get update \
	&& apt-get install -f \
	&& apt-get -y install mysql-server-5.7 \
	&& apt-get -y dist-upgrade \
	&& cd /build/freepbx \
	&& service mysql restart \
	&& ./start_asterisk start \
	&& ./install -n \
	&& fwconsole ma upgradeall \
	&& fwconsole ma downloadinstall soundlang asteriskinfo daynight configedit callforward callwaiting donotdisturb queues bulkhandler certman  \
	&& apt-get autoremove --purge -y \
	&& apt-get clean -y \
	&& rm -fr /var/lib/apt/lists/* \
	&& rm -fr /build 

FROM baseimage

COPY --from=builder /usr/lib/asterisk /usr/lib/asterisk 
COPY --from=builder /usr/lib/libasteriskssl.so.1 /usr/lib/libasteriskssl.so.1
COPY --from=builder /usr/lib/libasteriskssl.so /usr/lib/libasteriskssl.so
COPY --from=builder /usr/lib/libasteriskpj.so.2 /usr/lib/libasteriskpj.so.2
COPY --from=builder /usr/lib/libasteriskpj.so /usr/lib/libasteriskpj.so

COPY --from=builder  /usr/include/asterisk /usr/include/asterisk
COPY --from=builder  /usr/include/asterisk.h /usr/include/asterisk.h

COPY --from=builder  /var/lib/asterisk /var/lib/asterisk
COPY --from=builder  /var/spool/asterisk /var/spool/asterisk
COPY --from=builder  /var/run/asterisk /var/run/asterisk
COPY --from=builder  /var/log/asterisk /var/log/asterisk
COPY --from=builder /run/asterisk /run/asterisk
COPY --from=builder /home/asterisk/.gnupg /home/asterisk/.gnupg

# COPY --from=builder /etc/asterisk /etc/asterisk
COPY --from=builder /etc/amportal.conf /etc/amportal.conf
COPY --from=builder /etc/freepbx.conf /etc/freepbx.conf
COPY --from=builder /etc/init.d/asterisk /etc/init.d/asterisk
COPY --from=builder /usr/sbin/astdb2sqlite3 /usr/sbin/astdb2sqlite3
COPY --from=builder /usr/sbin/astcanary  /usr/sbin/astcanary
COPY --from=builder /usr/sbin/astman /usr/sbin/astman
COPY --from=builder /usr/sbin/astdb2bdb /usr/sbin/astdb2bdb
COPY --from=builder /usr/sbin/asterisk /usr/sbin/asterisk
COPY --from=builder /usr/sbin/rasterisk /usr/sbin/rasterisk
COPY --from=builder /usr/sbin/astversion /usr/sbin/astversion
COPY --from=builder /usr/sbin/astgenkey /usr/sbin/astgenkey
COPY --from=builder /usr/sbin/autosupport /usr/sbin/autosupport
COPY --from=builder /usr/sbin/safe_asterisk /usr/sbin/safe_asterisk
COPY --from=builder /var/www/html /var/www/html

# copy vrs confg
ADD mount/etc/asterisk /etc/asterisk
ADD src/vrs-ivvr /var/lib/asterisk/sounds/vrs-ivvr
ADD src/queue-ivvr /var/lib/asterisk/sounds
#ADD mount/lib/agi-bin/vrs_agi /var/lib/asterisk/agi-bin/vrs_agi
ADD src/codec/codec_g729.so /usr/lib/asterisk/modules/codec_g729.so
ADD src/codec/codec_opus.so /usr/lib/asterisk/modules/codec_opus.so
ADD src/codec/codec_opus_config-en_US.xml /var/lib/asterisk/documentation/thirdparty/codec_opus_config-en_US.xml

# setup env
ADD conf/www.conf /etc/php/5.6/fpm/pool.d/
ADD conf/nginx.conf /etc/nginx/

# freepbx, asterisk
RUN chmod +x /usr/lib/asterisk/modules/codec_g729.so \
	&& ln -s /var/lib/asterisk/bin/amportal /usr/sbin/amportal \
	&& ln -s /var/lib/asterisk/bin/fwconsole /usr/sbin/fwconsole 

# copy init script
ADD conf/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

ENTRYPOINT [ "/docker-entrypoint.sh" ]
