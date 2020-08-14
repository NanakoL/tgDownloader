'use strict';
const request = require('request-promise');
exports.main_handler = async (event) => {
    const options = JSON.parse(event.body);
    const res = await request(options);
    return {"status": 200, "result": res, "event": event}
};
