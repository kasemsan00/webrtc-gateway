The VoIP push notification for Asterisk AGI

# Requirement
- Python 2.7
- APNs library for Python, install using "easy_install apns" (see https://github.com/djacobs/PyAPNs)

# VoIPPayload class attributes:

* @param `token` a VoIP push notification token.
* @param `handle` a display name that show on ringing view.
* @param `sip` an information of SIP caller address.
* @param `video` make a voice or video call to the destination. The default is False (Optional).
* @param `sandbox` fire a push notification in sandbox mode.
* @return `cert` an enhanced VoIP credential path. The default is cert.pem.

# Usage example:

## Config your AGI

Generally, developer can uses the PushKit by filling all required parameters in asterisk dialplan.
```ssh
same => n,AGI(/var/lib/asterisk/agi-bin/pushkit_for_asterisk/pushkit.py, ${DB(SIP/Registry/${EXTEN})})
```

There are only one parameter that you need to provide the library including `registry` as `agi_arg_1`.
We note that the library get the `handle` and `sip` address automatically from asterisk agi environment. Thus, a developer dont need to provide these two parameters.

## Place your VoIP certificate

This library required the Apple Developer certificates, the descriptions are below.

- Generate a CSR from Keychain Access
- Generate a VoIP certificate from provision profile website.
- Import VoIP certificate Keychain Access 
- Export a p12 file from imported VoIP certificate, the p12 contains VoIP certificate and private key that is generated from CSR in the first step.
- Build a PEM file using command

```ssh
$ openssl pkcs12 -in cert.p12 -out cert.pem -nodes -clcerts
```

- Use the generated PEM place on this library folder 
(the default folder is `/var/lib/asterisk/agi-bin/pushkit_for_asterisk`).

## Execution permission

Change owner and permission of all library files.

```ssh
$ chown asterisk:asterisk -R pushkit_for_asterisk/
$ chmod 755 -R pushkit_for_asterisk/
```

## Run 

 The payload dictionary is look like..
 ```json
 {"aps":{"alert":"Incoming call...","sound":"default","badge": 1}, "handle": "IMALICE", "addr": "00013@203.150.245.41", "video": 1}
 ```

