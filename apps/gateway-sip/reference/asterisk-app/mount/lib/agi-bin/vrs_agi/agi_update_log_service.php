#!/usr/bin/php -q
<?php
require_once 'lib/phpagi.php';
require_once 'lib/func.db.php';

/*
* ARGV Fomat
* 
* Type 1 = Hangup before start queue
* (1,${USER_UNIQUEID},${USER_SOURCE_TYPE},${EPOCH})
*
* Type 2 = Agent Answer 
* (2,${USER_UNIQUEID},${USER_SOURCE_TYPE},${USER_HANGUP_TIME},${USER_QUEUE_TIME},${AGENT_EXTEN},${AGENT_TIME},${REC_STATUS}) 

* Type 3 = Update is_block=1 column(mysql) 
* (3,${USER_UNIQUEID},${USER_SOURCE_TYPE}) 
*/

$type = $argv[1];
$uniqueid = $argv[2];
$user_hangup_timestamp = $argv[4];
$source_type = $argv[3];

$tableName = strcmp($source_type, "TEXT") === 0 ? 'log_service_text' : 'log_service';

switch ($type) {
    case '3':
        $strSQL = "UPDATE
                        `{$tableName}`
                    SET
                        `is_block` = '1'
                    WHERE
                        `ls_uniqueid` = '{$uniqueid}'";

        dbquery($strSQL);

        break;

    case '2':
        $queue_begin_timestamp = $argv[5];
        $agent_answer_timestamp = $argv[7];
        $is_voice = ($argv[8]=='RECORDING')?'1':'0';

        $agent_number = !empty($argv[6])?'\''.$argv[6].'\'':'NULL';
        $queue_begin_datetime = date('Y-m-d H:i:s', $queue_begin_timestamp);

        if(!empty($agent_answer_timestamp))
            $service_duration = intval($user_hangup_timestamp) - intval($agent_answer_timestamp);
        else
            $service_duration = 0;

        $total_duration = intval($user_hangup_timestamp) - intval($queue_begin_timestamp);
        
        /* Total Duration Round */
        if($total_duration == 0)
            $total_duration = 1;

        $waiting_duration = intval($total_duration) -  intval($service_duration);

        $strSQL = "UPDATE
                        `{$tableName}`
                    SET
                        `agent` = {$agent_number},
                        `queue_begin` = '{$queue_begin_datetime}',
                        `waiting_duration` = {$waiting_duration},
                        `service_duration` = {$service_duration},
                        `total_duration` = {$total_duration},
                        `is_voice` = '{$is_voice}'
                    WHERE
                        `ls_uniqueid` = '{$uniqueid}'";

        dbquery($strSQL);

        break;

    case '1':
        $total_duration = intval($user_hangup_timestamp) - intval($uniqueid);

        $strSQL = "UPDATE
                        `{$tableName}`
                    SET
                        `total_duration` = {$total_duration}
                    WHERE
                        `ls_uniqueid` = '{$uniqueid}'";
                        
        dbquery($strSQL);

        break;

    default:
        # code...
        break;
}

return 0;
?>
