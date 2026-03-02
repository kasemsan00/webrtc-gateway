#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""
A tools for the VoIP push notification for Asterisk
Developed by Kiettiphong Manovisut
"""

def extract_registry(payload=None):
	if payload:
		if 'pn-type=apple' in payload.registry:
			kvs = payload.registry.split(';')
			for kv in kvs:
				try:
					k, v = kv.split('=')
					if k and v:
						if 'app-id' in k and '.dev' in v:
							payload.sandbox = True
						elif 'pn-tok' in k:
							payload.token = v
				except Exception as e:
					# Some objects are not contain equal sign (=)
					pass
	return payload