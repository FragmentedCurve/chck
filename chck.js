var chck = {
    'config': {
	'icons': {
	    'on': '✅',
	    'off': '❌',
	    'dead': '💀',
	    'unauthorized': '🔒 <input name="chck-password" text="text" placeholder="Password..." />'
	},
	'custom_icons': {},

	'api': {
	    'uri': ''
	},
    },

    'api': {
	'request': function(method, id, password, callback) {
	    let req = new XMLHttpRequest();

	    req.onreadystatechange = function () {
		let state = null;
		
		if (req.readyState == 4) {
		    switch (req.status) {
		    case 200:
			if (req.response == '1')
			    state = 'on';
			else
			    state = 'off';
			break;
		    case 404:
			state = 'dead'
			break;
		    case 401:
			state = 'unauthorized';
			break;
		    }

		    callback(state);
		}
	    }

	    req.open(method, chck.config.api.uri + id, true);
	    req.send(password);
	},

	'fetch': function(id, callback) {
	    chck.api.request("GET", id, null, callback);
	},

	'toggle': function(id, password, callback) {
	    chck.api.request("PUT", id, password, callback);
	}
    },

    'tag': {
	'id': function(element) {
	    return element.getAttribute('value');
	},

	'getPasswordTag': function(element) {
	    const inputs = element.getElementsByTagName('input');
	    for (const e of inputs)
		if (e.name === 'chck-password')
		    return e;
	    return undefined;
	},
	
	'password': function(element) {	    
	    if (element.state == 'unauthorized') {
		const input = chck.tag.getPasswordTag(element);
		if (input !== undefined) 
		    return input.value;
	    }
	    
	    return element.getAttribute('password');
	},

	'refresh': function(element) {
	    chck.api.fetch(
		chck.tag.id(element),
		(state) => {
		    chck.tag.update(element, state);
		}
	    );
	},
	
	'update': function(element, state) {
	    const id = chck.tag.id(element);
	    let icon_update = chck.config.icons[state];

	    if (id in chck.config.custom_icons && state in chck.config.custom_icons[id])
		icon_update = chck.config.custom_icons[id][state];

	    element.innerHTML = icon_update;
	    element.state = state;

	    element.onclick = () => {
		chck.api.toggle(id, null, (state) => { chck.tag.update(element, state); });
	    };
	    
	    if (state == 'unauthorized') {
		let input = chck.tag.getPasswordTag(element);

		if (input === undefined) {
		    // If there's no password prompt, refresh the chck
		    // after a timeout.
		    setTimeout(() => {
			chck.tag.refresh(element);
		    }, 1000);
		} else {
		    element.onclick = null;
		    input.focus();

		    input.onblur = (event) => {
			chck.tag.refresh(element);
		    };
		    
		    input.onkeydown = (event) => {
			if (event.key == 'Enter') {
			    const pw = chck.tag.password(element);
			    chck.api.toggle(id, pw, (state) => {
				chck.tag.update(element, state);
				chck.tag.refresh(element);
			    });
			}
		    };
		}
	    }
	},
    },
    
    'page': {
	'refresh': function() {
	    for (const e of document.getElementsByTagName('chck'))
		chck.tag.refresh(e);
	}
    },
};
