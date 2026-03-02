#!/usr/bin/php -q
<?php
  include 'url.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();
  $curl = curl_init();

  curl_setopt_array($curl, array(
    CURLOPT_URL => $url_destroy_mcu_room."?action=delete&room=0".$agi->get_variable('CALLERID(num)')['data'],
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_ENCODING => "",
    CURLOPT_MAXREDIRS => 10,
    CURLOPT_TIMEOUT => 0,
    CURLOPT_FOLLOWLOCATION => true,
    CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
    CURLOPT_CUSTOMREQUEST => "GET",
  ));

  $response = curl_exec($curl);
  curl_close($curl);
  return 0;
?>
