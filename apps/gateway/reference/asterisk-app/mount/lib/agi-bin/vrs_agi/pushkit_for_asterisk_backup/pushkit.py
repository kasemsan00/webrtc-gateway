#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""
A tools for the VoIP push notification for Asterisk
Developed by Kiettiphong Manovisut
"""

from asterisk.agi import *
agi = AGI()

import time
import sys
import os
from apns import APNs, Frame, Payload
from voip_payload import VoIPPayload
from utils import extract_registry


if __name__ == "__main__":

	def agi_verbose(msg):
		agi.verbose('[ PushKit ]: %s' % msg)

	def push_payload(payload):
		# Converting Python's boolean into binary.
		req_video = 0 
		if payload.video is True:
			req_video = 1

		payload.dict()

		# Send an iOS 10 compatible notification
		apns = APNs(use_sandbox=payload.sandbox, cert_file=payload.cert, enhanced=True)
		apns_payload = Payload(alert="Incoming call...", sound="default", badge=1, custom={"handle":payload.handle, "addr":payload.sip, "video":req_video})
		apns.gateway_server.send_notification(payload.token, apns_payload)
		agi_verbose(apns_payload)
		agi_verbose("pushkit is completed")

		# disable APNS connection and error-responses handler immediately.
		apns.gateway_server.force_close() 

	try:
		agi_verbose("create a payload...")
		payload = VoIPPayload()
		payload.cert = '%s/%s' % (os.path.dirname(os.path.abspath(__file__)), 'cert.pem')
		payload.sip = agi.env['agi_callerid']
		payload.handle = agi.env['agi_calleridname']
		payload.registry = agi.env['agi_arg_1']
		
		if payload.handle and payload.sip:
			if os.path.exists(payload.cert):
				# Extracting asterisk registry string first.
				if payload.registry:
					payload = extract_registry(payload)
					if payload is not None and payload.token:
						push_payload(payload)
					else:
						agi_verbose('The asterisk registry string is invalid ! (eg. pn-type, token, app-id)')
				# Secondly, using the token instead.
				elif payload.token is not None:
					push_payload(payload)
				else:
					agi_verbose('A VoIP token is not provided !')
			else:
				agi_verbose('Can\'t find any VoIP certificate (./cert.pem) !')
		else:
			agi_verbose('The input parameter from AGI is invalid !')
	except Exception as e:
		agi.verbose('%s' % e)