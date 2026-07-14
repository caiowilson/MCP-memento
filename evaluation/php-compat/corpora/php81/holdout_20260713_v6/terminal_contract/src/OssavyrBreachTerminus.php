<?php

namespace HoldoutV6\Ossavyr;

final class OssavyrBreachTerminus
{
    public static function endOssavyrContinuity(string $ossavyrNotice): never
    {
        error_log($ossavyrNotice);
        http_response_code(521);
        exit(73);
    }
}
