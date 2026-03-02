#!/usr/bin/php -q
<?php

require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

$agi = new AGI();

$boundType = $argv[1];
$sourceType = $argv[2];
$serviceIQMID = ConvertSourceTypeID($sourceType);
$sourceNumber = $argv[3];
$destinationNumber = $argv[4];

// request check block
$subPath = GetPathParams($boundType, $serviceIQMID, $sourceNumber, $destinationNumber);
$agi->verbose("Sub Path: " . $subPath);
$response = CurlEvent("http://127.0.0.1:8021/", $subPath);
$agi->verbose("response: " . $response[response]);
$res = json_decode($response[response], true);
$agi->verbose("res: " . $res);
$agi->verbose("data: " . $res['data']);
$agi->verbose("isBlock: " . $res['data']['isBlock']);

// set isBlock
if ($res['data']['isBlock'] === true) {
    $agi->set_variable('BLOCK', '1');
} elseif ($res['data']['isBlock'] === false) {
    $agi->set_variable('BLOCK', '0');
} else {
    $agi->set_variable('BLOCK', 'error');
}

return 0;

function CurlEvent($domain, $route) {
    try {
        $curl = curl_init();

        curl_setopt_array($curl, array(
        CURLOPT_URL => $domain . $route,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_ENCODING => "",
        CURLOPT_MAXREDIRS => 10,
        CURLOPT_TIMEOUT => 3,
        CURLOPT_CONNECTTIMEOUT => 1,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_HTTP_VERSION => CURL_HTTP_VERSION_1_1,
        CURLOPT_CUSTOMREQUEST => "GET",
        ));

        $response[domain] = $domain;
        $response[route] = $route;
        $response[response] = curl_exec($curl);
        $response[httpCode] = curl_getinfo($curl, CURLINFO_HTTP_CODE);
        curl_close($curl);
        return $response;
    }
    catch(Exception $err) {
        $agi->verbose("[ERROR] agi check block: " . $err);
        $response[response] = $err;
        return $response;
    }
}

function GetPathParams($boundType, $serviceID, $srcNum, $desNum) {
    if ($srcNum != "" && $desNum != "")
        return "block/{$boundType}/service/{$serviceID}/source/{$srcNum}/destination/{$desNum}";
    elseif ($srcNum != "")
        return "block/{$boundType}/service/{$serviceID}/source/{$srcNum}";
    elseif ($desNum != "")
        return "block/{$boundType}/service/{$serviceID}/destination/{$desNum}";

	return "";
}

function ConvertSourceTypeID($sourceType) {
    if ($sourceType == 'KIOSK')
        return "6";
    elseif ($sourceType == 'VP')
        return "9";
    elseif ($sourceType == 'WEB-PC')
        return "12";
    elseif ($sourceType == 'WEB-MOBILE')
        return "13";
    elseif ($sourceType == 'CAPTION')
        return "8";
    elseif ($sourceType == 'MOBILE')
        return "15";
    elseif ($sourceType == 'WEB-PUBLIC')
        return "17";
    elseif ($sourceType == 'MOBILE-PUBLIC')
        return "18";
    elseif ($sourceType == '1412')
        return "19";
    else 
        return "2";

    // default Message service id 2
	return "2";
}

?>
