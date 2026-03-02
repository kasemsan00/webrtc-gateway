#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();
  $destination_number = $agi->get_variable("EXTEN")["data"];
  $agi->verbose($destination_number);

  try {
    $curl = curl_init();

    curl_setopt_array($curl, array(
      CURLOPT_URL => $domain_ttrs_number_detail . $destination_number,
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
    
    if ($responseJSON["status"] == "OK" && $responseJSON["data"] != null) {
      $str = $responseJSON["data"]["first_name"] . " " . $responseJSON["data"]["last_name"];

      if (strlen($str) <= 255) {
        $agi->set_variable('ttrs_number_name', $str);
      }
    }
  }
  catch(Exception $err) {
    $agi->verbose($err);
  }

  return 0;
?>
