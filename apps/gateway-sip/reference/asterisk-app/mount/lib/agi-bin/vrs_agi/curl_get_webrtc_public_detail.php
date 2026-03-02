#!/usr/bin/php -q
<?php
  require_once 'env_for_agi.php';
  require_once 'lib/phpagi.php';
  require_once 'lib/func.db.php';

  $agi = new AGI();
  $number = $agi->get_variable("CALLERID(num)")["data"];

  try {
    $opts = array('http' =>
      array(
          'method'  => 'GET',
          'timeout' => 2
      )
    );
    $context  = stream_context_create($opts);
    $result = file_get_contents($domain_public_detail."?extension=$number", false, $context);
    $responseJSON = json_decode($result);

    if ($responseJSON->status == "OK" && $responseJSON->response != null) {
      $str = $responseJSON->response->agency . " " . $responseJSON->response->name . " " . $responseJSON->response->lastname;

      if (strlen($str) <= 255) {
        $agi->set_variable('webrtc_public_name', $str);
      }
    }

    $agi->verbose($responseJSON->response);

    $agi->set_variable('__card_id', $responseJSON->response->identification);
    $agi->set_variable('__mobile_UID', $responseJSON->response->mobileUID);
  }
  catch(Exception $err) {
    $agi->verbose($err);
  }

  return 0;
?>
