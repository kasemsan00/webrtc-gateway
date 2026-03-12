#!/usr/bin/env python
# -*- coding: utf-8 -*-

"""
A namespace class for the VoIP push notification for Asterisk
Developed by Kiettiphong Manovisut
"""

class VoIPPayload:

    def __init__(self, **kwargs):
        self.__dict__.update(
            kwargs,
            token=None, 
            handle=None, 
            sip=None, 
            registry=None, 
            video=True, 
            sandbox=False, 
            cert=None, 
            cwd=None
        )

    def dict(self):
        return '\n'.join(["obj.%s = %r" % (attr, getattr(self, attr)) for attr in dir(self)])
