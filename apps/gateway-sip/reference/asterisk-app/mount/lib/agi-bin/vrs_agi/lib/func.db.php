<?php

define('DB_NOT_FETCH', 0);
define('DB_FETCH_ALL', 1);
define('DB_FETCH_ROW', 2);

define('DB_QUIET', 0);
define('DB_DISPLAY_ERROR', 1);

define('DB_USERNAME', 'root');
define('DB_PASSWORD', 'gog8m8@96e7eb3c4d96836e7ecd');
define('DB_SERVER', '127.0.0.1');
//define('DB_DATABASE', 'vrslog_v2');
define('DB_DATABASE', 'vrslog');

$conn = mysqli_connect(DB_SERVER, DB_USERNAME, DB_PASSWORD, DB_DATABASE) or die("Can't connect to database");
mysqli_select_db($conn, DB_DATABASE) or die("Can't select database");
mysqli_query($conn, "SET NAMES UTF8");

function dbquery($strSQL, $type = DB_NOT_FETCH, $debug = DB_DISPLAY_ERROR) {
    global $conn;

    if (empty($conn)) {
	$conn = mysqli_connect(DB_SERVER, DB_USERNAME, DB_PASSWORD, DB_DATABASE) or die("Can't connect to database");
	mysqli_select_db($conn, DB_DATABASE) or die("Can't select database");
	mysqli_query($conn, "SET NAMES UTF8");
    }
    $result = mysqli_query($conn, $strSQL);

    if ($debug && !$result)
	die("QUERY ERROR: " . mysqli_error($conn));

    if ($type == DB_FETCH_ALL) {
	$output = array();
	while ($rs = mysqli_fetch_assoc($result)) {
	    array_push($output, $rs);
	}
	return $output;
    } elseif ($type == DB_FETCH_ROW) {
	if (mysqli_num_rows($result) > 0) {
	    return mysqli_fetch_assoc($result);
	} else {
	    return false;
	}
    } else {
	return true;
    }
}

?>
