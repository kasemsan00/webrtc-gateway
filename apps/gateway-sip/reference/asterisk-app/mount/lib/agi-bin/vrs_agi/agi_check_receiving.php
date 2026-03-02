#!/usr/bin/php -q
<?php
    require_once 'env_for_agi.php';
    require_once 'lib/phpagi.php';
    require_once 'lib/func.db.php';

    date_default_timezone_set('Asia/Bangkok');

    $agi = new AGI();

    $agi->verbose("###################################### AGI ######################################");

    // $add = "PT".$default[time]."M";
    $add = "P".$default[time]."D";

    $date = new DateTime("now");
    $date->sub(new DateInterval($add));
    $date = $date->format('Y-m-d H:i:s');

    // $number="022180016";
    $number = $agi->get_variable("USER_ID")["data"];
    
    $strSQL = "SELECT * FROM `log_dial` WHERE `destination` = '$number' AND `last_update` >= '$date' ORDER BY `last_update` DESC LIMIT 1";
    $agi->verbose("###################################### Query ######################################");
    $agi->verbose($strSQL);

    $res = dbquery($strSQL, DB_FETCH_ROW);

    $video = "MOBILE";
    $text = "TEXT";

    $agi->verbose($res);

    if (empty($res)) {
        if ($default[checkReceiving] == "video") {
            $agi->set_variable('receiving_type', $video);
        } elseif ($default[checkReceiving] == "text") {
            $agi->set_variable('receiving_type', $text);
        } else {
            $agi->set_variable('receiving_type', $video);
        }
    } else {
        $substring = substr($res[source], 0, 2);
        $agi->verbose($substring);
        if ($substring == "70") {
            $agi->set_variable('receiving_type', $video);
        } elseif ($substring == "80") {
            $agi->set_variable('receiving_type', $text);
        } else {
            if ($default[checkReceiving] == "video") {
                $agi->set_variable('receiving_type', $video);
            } elseif ($default[checkReceiving] == "text") {
                $agi->set_variable('receiving_type', $text);
            } else {
                $agi->set_variable('receiving_type', $video);
            }
        }
    }

    return 0;
?>
