#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();
  $agent = $agi->get_variable("OPERATOR_CALLERID")["data"];
  $destination = $agi->get_variable("EXTEN")["data"];

  try {
    $curl = curl_init();

    curl_setopt_array($curl, array(
      CURLOPT_URL => $domain_ttrs_number . "/user/ttrsnumber?mobile=" . $destination . "&agent=" . $agent,
      CURLOPT_RETURNTRANSFER => true,
      CURLOPT_ENCODING => "",
      CURLOPT_MAXREDIRS => 10,
      CURLOPT_TIMEOUT => 3,
      CURLOPT_CONNECTTIMEOUT => 1,
      CURLOPT_FOLLOWLOCATION => true,
      CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
      CURLOPT_CUSTOMREQUEST => "GET",
    ));

    $response = curl_exec($curl);
    $httpCode = curl_getinfo($curl, CURLINFO_HTTP_CODE);
    curl_close($curl);

    $responseJSON = json_decode($response, true);
    
    if ($responseJSON["status"] == "OK" && $responseJSON["data"]["ttrs_number"] != null) {
      $agi->set_variable('ttrs_number', $responseJSON["data"]["ttrs_number"]);
    }
  }
  catch(Exception $err) {
    $agi->verbose($err);
  }

  return 0;
?>
