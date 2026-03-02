<?php
  // dev
  //$url_popup = "http://203.151.85.177:4400";
  //$domain_event = "http://203.151.85.177:4400/";
  //$domain_public_detail = "https://dev-vrswebapi.ttrs.or.th/publicdetail";
  //$domain_ttrs_number_detail = "https://api.ttrs.or.th/v3/accounts/ttrs-number/";

  // prod
  //$url_popup = "http://127.0.0.1:4400";
  //$domain_event = "http://127.0.0.1:4400/";
  //$domain_public_detail = "https://vrswebapi.ttrs.or.th/publicdetail";
  // $domain_public_detail = "https://dev-vrswebapi.kasemsan.net/publicdetail";
  //$domain_ttrs_number_detail = "https://api.ttrs.or.th/v3/accounts/ttrs-number/";
  //$domain_ttrs_number = "http://203.150.245.35:4444";

  $url_popup = "";
  $domain_event = "";
  $domain_public_detail = "";
  $domain_ttrs_number_detail = "";
  $domain_ttrs_number = "";

  $default = array(
    "time"   =>  3,
    "checkReceiving"   =>  "video"
  );

  $queue_for_icrm = array(
    "001"   =>  1,
    "0011"  =>  1,
    "002"   =>  1,
    "003"   =>  1,
    "004"   =>  1,
    "005"   =>  1,
    "009"   =>  1,
    "1199"  =>  1,
    "210"   =>  1,
    "211"   =>  1,
    "3201"  =>  0,
    "3202"  =>  0,
    "810"   =>  1,
    "811"   =>  1,
    "812"   =>  1,
    "813"   =>  1,
    "814"   =>  0,
    "815"   =>  0,
    "816"   =>  0,
    "850"   =>  0,
    "890"   =>  1,
    "910"   =>  0
  );

  //$domain_event_live_chat = "https://apilivechat.ttrs.or.th/receiving/";
  $domain_event_live_chat = "";
  $queue_for_live_chat = array(
    "910"   =>  1
  );
?>
